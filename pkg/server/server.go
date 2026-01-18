package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/abilashraghuram/cbox/out/gen/chvapi"
	"github.com/abilashraghuram/cbox/out/gen/serverapi"
	"github.com/abilashraghuram/cbox/pkg/callback"
	"github.com/abilashraghuram/cbox/pkg/cmdserver"
	"github.com/abilashraghuram/cbox/pkg/config"
	"github.com/abilashraghuram/cbox/pkg/server/cidallocator"
	"github.com/abilashraghuram/cbox/pkg/server/fountain"
	"github.com/abilashraghuram/cbox/pkg/server/ipallocator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gvisor.dev/gvisor/pkg/cleanup"
)

// vmStatus represents the status of a VM.
type vmStatus int

const (
	vmStatusCreated vmStatus = iota
	vmStatusRunning
	vmStatusStopped
)

func (status vmStatus) String() string {
	switch status {
	case vmStatusCreated:
		return "CREATED"
	case vmStatusRunning:
		return "RUNNING"
	case vmStatusStopped:
		return "STOPPED"
	default:
		return "UNKNOWN"
	}
}

const (
	serialPortMode  = "Tty"
	consolePortMode = "Off"

	numNetDeviceQueues      = 2
	netDeviceQueueSizeBytes = 256
	netDeviceId             = "_net0"
	reapVmTimeout           = 20 * time.Second

	cidAllocatorLow  = 3
	cidAllocatorHigh = 1000

	statefulDiskFilename      = "stateful.img"
	minGuestMemoryMB          = 1024
	maxGuestMemoryMB          = 32768
	defaultGuestMemPercentage = 50

	cmdServerReadyTimeout    = 1 * time.Minute
	cmdServerReadyRetryDelay = 10 * time.Millisecond
)

func String(s string) *string {
	return &s
}

func Int32(i int32) *int32 {
	return &i
}

func Bool(b bool) *bool {
	return &b
}

type vm struct {
	lock             sync.RWMutex
	name             string
	stateDirPath     string
	apiSocketPath    string
	apiClient        *chvapi.APIClient
	process          *os.Process
	ip               *net.IPNet
	tapDevice        *fountain.TapDevice
	status           vmStatus
	vsockPath        string
	cid              uint32
	statefulDiskPath string
}

// Server manages VMs with exec and callback capabilities.
type Server struct {
	lock           sync.RWMutex
	vms            map[string]*vm
	fountain       *fountain.Fountain
	ipAllocator    *ipallocator.IPAllocator
	cidAllocator   *cidallocator.CIDAllocator
	config         config.ServerConfig
	sessionManager *callback.SessionManager
}

// calculateVCPUCount returns an appropriate number of vCPUs based on host's CPU count.
func calculateVCPUCount() int32 {
	hostCPUs := int32(runtime.NumCPU())
	minVCPUs := int32(1)
	maxVCPUs := int32(8)
	suggestedVCPUs := hostCPUs / 2

	if suggestedVCPUs < minVCPUs {
		return minVCPUs
	}
	if suggestedVCPUs > maxVCPUs {
		return maxVCPUs
	}
	return suggestedVCPUs
}

// calculateGuestMemorySizeInMB calculates the appropriate memory size for the guest.
func calculateGuestMemorySizeInMB(memoryPercentage int32) (int32, error) {
	if memoryPercentage <= 0 || memoryPercentage > 100 {
		memoryPercentage = defaultGuestMemPercentage
		log.Warnf(
			"Invalid memory percentage provided: %d, using default of %d%%",
			memoryPercentage,
			defaultGuestMemPercentage,
		)
	}

	var totalMemoryKB int64
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		log.Warn("Could not determine host memory size, using default of 4096 MB")
		return minGuestMemoryMB, nil
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				memKB, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					totalMemoryKB = memKB
					break
				}
			}
		}
	}
	if totalMemoryKB <= 0 {
		return 0, fmt.Errorf("could not determine host memory size")
	}
	log.Infof("Total host memory: %d MB", totalMemoryKB/1024)

	suggestedMemoryKB := (totalMemoryKB * int64(memoryPercentage)) / 100
	if suggestedMemoryKB < minGuestMemoryMB*1024 {
		return 0, fmt.Errorf(
			"host memory allocation too small. suggested memory: %d MB (at %d%%) total memory: %d MB",
			suggestedMemoryKB/1024,
			memoryPercentage,
			totalMemoryKB/1024,
		)
	}
	if suggestedMemoryKB > maxGuestMemoryMB*1024 {
		return maxGuestMemoryMB, nil
	}
	return int32(suggestedMemoryKB / 1024), nil
}

func getKernelCmdLine(gatewayIP string, guestIP string, vmName string) string {
	return fmt.Sprintf(
		"console=ttyS0 gateway_ip=\"%s\" guest_ip=\"%s\" vm_name=\"%s\"",
		gatewayIP,
		guestIP,
		vmName,
	)
}

// bridgeExists checks if a bridge with the given name exists.
func bridgeExists(bridgeName string) (bool, error) {
	cmd := exec.Command("ip", "link", "show", "type", "bridge")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("error executing command: %v", err)
	}

	bridges := strings.Split(string(output), "\n")
	for _, bridge := range bridges {
		if strings.Contains(bridge, bridgeName+":") {
			return true, nil
		}
	}
	return false, nil
}

func cleanupAllIPTablesRulesForIP(ip string) error {
	log.Infof("deleting all iptables rules for IP: %s", ip)
	cmd := exec.Command("iptables", "-t", "nat", "-L", "PREROUTING", "-n", "--line-numbers")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list iptables rules: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	var ruleNumbers []int

	for i := 2; i < len(lines); i++ {
		line := lines[i]
		if strings.Contains(line, "to:"+ip+":") {
			log.Infof("deleting rule: %s", line)
			fields := strings.Fields(line)
			if len(fields) > 0 {
				ruleNum, err := strconv.Atoi(fields[0])
				if err == nil {
					ruleNumbers = append(ruleNumbers, ruleNum)
				}
			}
		}
	}

	sort.Sort(sort.Reverse(sort.IntSlice(ruleNumbers)))

	var finalErr error
	for _, ruleNum := range ruleNumbers {
		cmd := exec.Command(
			"iptables",
			"-t",
			"nat",
			"-D",
			"PREROUTING",
			strconv.Itoa(ruleNum),
		)

		if err := cmd.Run(); err != nil {
			log.Warnf("error deleting iptables rule %d for IP %s: %v", ruleNum, ip, err)
			finalErr = errors.Join(
				finalErr,
				fmt.Errorf("failed to delete rule %d: %w", ruleNum, err),
			)
		}
	}
	return finalErr
}

func cleanupTapDevices() error {
	interfaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("failed to list interfaces: %v", err)
	}

	for _, iface := range interfaces {
		if strings.HasPrefix(iface.Name, "tap") {
			if err := exec.Command("ip", "link", "delete", iface.Name).Run(); err != nil {
				log.Warnf("failed to delete tap device %s: %v", iface.Name, err)
			}
			log.Infof("deleted tap device: %s", iface.Name)
		}
	}
	return nil
}

func cleanupBridge() error {
	_, err := exec.Command("ip", "link", "show", "br0").CombinedOutput()
	if err != nil {
		return nil
	}

	if err := exec.Command("ip", "link", "delete", "br0").Run(); err != nil {
		return fmt.Errorf("failed to delete bridge br0: %v", err)
	}
	log.Info("deleted bridge: br0")
	return nil
}

// setupBridgeAndFirewall sets up a bridge and firewall rules.
func setupBridgeAndFirewall(
	backupFile string,
	bridgeName string,
	bridgeIP string,
	bridgeSubnet string,
) error {
	output, err := exec.Command("iptables-save").Output()
	if err != nil {
		return fmt.Errorf("failed to run iptables-save: %w", err)
	}

	err = os.WriteFile(backupFile, output, 0644)
	if err != nil {
		return fmt.Errorf("failed to save iptables-save to: %v: %w", backupFile, err)
	}

	output, err = exec.Command("sh", "-c", "ip r | grep default | awk '{print $5}'").Output()
	if err != nil {
		return fmt.Errorf("failed to get default network interface: %w", err)
	}
	hostDefaultNetworkInterface := strings.TrimSpace(string(output))

	exists, err := bridgeExists(bridgeName)
	if err != nil {
		return fmt.Errorf("failed to detect if bridge exists: %w", err)
	}

	if exists {
		log.Info("networking already setup")
		return nil
	}

	commands := []struct {
		name string
		args []string
	}{
		{"ip", []string{"l", "add", bridgeName, "type", "bridge"}},
		{"ip", []string{"l", "set", bridgeName, "up"}},
		{"ip", []string{"a", "add", bridgeIP, "dev", bridgeName, "scope", "host"}},
		{"iptables", []string{"-t", "nat", "-A", "POSTROUTING", "-s", bridgeSubnet, "-o", hostDefaultNetworkInterface, "-j", "MASQUERADE"}},
		{"sysctl", []string{"-w", fmt.Sprintf("net.ipv4.conf.%s.forwarding=1", hostDefaultNetworkInterface)}},
		{"sysctl", []string{"-w", fmt.Sprintf("net.ipv4.conf.%s.forwarding=1", bridgeName)}},
		{"iptables", []string{"-t", "filter", "-I", "FORWARD", "-s", bridgeSubnet, "-j", "ACCEPT"}},
		{"iptables", []string{"-t", "filter", "-I", "FORWARD", "-d", bridgeSubnet, "-j", "ACCEPT"}},
	}

	for _, cmd := range commands {
		if err := exec.Command(cmd.name, cmd.args...).Run(); err != nil {
			return fmt.Errorf("failed to execute command '%s %s': %w", cmd.name, strings.Join(cmd.args, " "), err)
		}
	}

	return nil
}

func getVmStateDirPath(stateDir string, vmName string) string {
	return path.Join(stateDir, vmName)
}

func getVmSocketPath(vmStateDir string, vmName string) string {
	return path.Join(vmStateDir, vmName+".sock")
}

func unixSocketClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: time.Second * 30,
	}
}

func createApiClient(apiSocketPath string) *chvapi.APIClient {
	configuration := chvapi.NewConfiguration()
	configuration.HTTPClient = unixSocketClient(apiSocketPath)
	configuration.Servers = chvapi.ServerConfigurations{
		{
			URL: "http://localhost/api/v1",
		},
	}
	return chvapi.NewAPIClient(configuration)
}

func waitForServer(ctx context.Context, apiClient *chvapi.APIClient, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			default:
				resp, r, err := apiClient.DefaultAPI.VmmPingGet(ctx).Execute()
				if err == nil {
					log.WithFields(log.Fields{
						"buildVersion": *resp.BuildVersion,
						"statusCode":   r.StatusCode,
					}).Info("cloud-hypervisor server up")
					errCh <- nil
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	return <-errCh
}

func reapProcess(process *os.Process, logger *log.Entry, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		log.Info("waiting for VM process to exit")
		_, err := process.Wait()
		done <- err
	}()

	select {
	case err := <-done:
		logger.Infof("VM process exited via wait")
		return err
	case <-time.After(timeout):
		logger.Warnf("Timeout waiting for VM process to exit")
	}

	err := process.Kill()
	if err != nil {
		return fmt.Errorf("failed to kill VM process: %v", err)
	}
	return fmt.Errorf("VM process was force killed after timeout")
}

// getIPPrefix returns the IP prefix from the given CIDR.
func getIPPrefix(cidr string) (string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("failed to parse CIDR: %w", err)
	}

	ones, _ := ipNet.Mask.Size()
	completeOctets := ones / 8
	octets := strings.Split(ipNet.IP.String(), ".")

	if completeOctets > 0 && completeOctets <= len(octets) {
		return strings.Join(octets[:completeOctets], "."), nil
	}

	return "", fmt.Errorf("invalid mask size: %d", ones)
}

func createStatefulDisk(path string, sizeInMB int32) error {
	log.Infof("Creating stateful disk at %s with size %dMB", path, sizeInMB)
	cmd := exec.Command(
		"truncate",
		"-s",
		fmt.Sprintf("%dM", sizeInMB),
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create stateful disk: %w out: %s", err, string(out))
	}

	cmd = exec.Command("mkfs.ext4", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to format stateful disk with ext4: %w out: %s", err, string(out))
	}
	return nil
}

// NewServer creates a new Server instance.
func NewServer(config config.ServerConfig, sessionManager *callback.SessionManager) (*Server, error) {
	if err := cleanupTapDevices(); err != nil {
		return nil, fmt.Errorf("failed to cleanup tap devices: %w", err)
	}

	if err := cleanupBridge(); err != nil {
		return nil, fmt.Errorf("failed to cleanup bridge: %w", err)
	}

	ipPrefix, err := getIPPrefix(config.BridgeSubnet)
	if err != nil {
		return nil, fmt.Errorf("failed to get IP prefix: %w", err)
	}

	log.Infof("Cleaning up iptables rules for IP prefix: %s", ipPrefix)
	if err := cleanupAllIPTablesRulesForIP(ipPrefix); err != nil {
		return nil, fmt.Errorf("failed to cleanup iptables rules: %w", err)
	}

	if err := os.MkdirAll(config.StateDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create vm state dir: %v err: %w", config.StateDir, err)
	}

	ipBackupFile := fmt.Sprintf("/tmp/iptables-backup-%s.rules", time.Now().Format(time.UnixDate))
	if err := setupBridgeAndFirewall(
		ipBackupFile,
		config.BridgeName,
		config.BridgeIP,
		config.BridgeSubnet,
	); err != nil {
		return nil, fmt.Errorf("failed to setup networking on the host: %w", err)
	}

	ipAllocator, err := ipallocator.NewIPAllocator(config.BridgeSubnet)
	if err != nil {
		return nil, fmt.Errorf("failed to create ip allocator: %w", err)
	}

	cidAllocator, err := cidallocator.NewCIDAllocator(cidAllocatorLow, cidAllocatorHigh)
	if err != nil {
		return nil, fmt.Errorf("failed to create CID allocator: %w", err)
	}

	log.Infof("Server config: %+v", config)
	return &Server{
		vms:            make(map[string]*vm),
		fountain:       fountain.NewFountain(config.BridgeName),
		ipAllocator:    ipAllocator,
		cidAllocator:   cidAllocator,
		config:         config,
		sessionManager: sessionManager,
	}, nil
}

// GetVMNameByCID returns the VM name for the given CID.
func (s *Server) GetVMNameByCID(cid uint32) (string, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	for name, vm := range s.vms {
		if vm.cid == cid {
			return name, nil
		}
	}
	return "", fmt.Errorf("no VM found for CID %d", cid)
}

func (s *Server) getVMAtomic(vmName string) *vm {
	s.lock.RLock()
	defer s.lock.RUnlock()

	vm, exists := s.vms[vmName]
	if !exists {
		return nil
	}
	return vm
}

func (s *Server) createVM(
	ctx context.Context,
	vmName string,
	kernelPath string,
	initramfsPath string,
	rootfsPath string,
) (*vm, error) {
	cleanup := cleanup.Make(func() {
		log.WithFields(
			log.Fields{
				"vmname": vmName,
				"action": "cleanup",
				"api":    "createVM",
			},
		).Info("clean up done")
	})

	defer func() {
		cleanup.Clean()
	}()

	vmStateDir := getVmStateDirPath(s.config.StateDir, vmName)
	err := os.MkdirAll(vmStateDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create vm state dir: %w", err)
	}
	cleanup.Add(func() {
		if err := os.RemoveAll(vmStateDir); err != nil {
			log.WithError(err).Errorf("failed to remove vm state dir: %s", vmStateDir)
		}
	})
	log.Infof("CREATED: %v", vmStateDir)

	apiSocketPath := getVmSocketPath(vmStateDir, vmName)
	apiClient := createApiClient(apiSocketPath)

	logFilePath := path.Join(vmStateDir, "log")
	logFile, err := os.Create(logFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	cmd := exec.Command(s.config.ChvBinPath, "--api-socket", apiSocketPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("error spawning vm: %w", err)
	}
	cleanup.Add(func() {
		log.WithFields(log.Fields{"vmname": vmName, "action": "cleanup", "api": "createVM"}).Info("reap VMM process")
		reapProcess(cmd.Process, log.WithField("vmname", vmName), reapVmTimeout)
	})

	err = waitForServer(ctx, apiClient, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("error waiting for vm: %w", err)
	}
	cleanup.Add(func() {
		log.WithFields(log.Fields{"vmname": vmName, "action": "cleanup", "api": "createVM"}).Info("kill VMM process")
		if err := cmd.Process.Kill(); err != nil {
			log.WithField("vmname", vmName).Errorf("Error killing vm: %v", err)
		}
	})
	log.WithField("vmname", vmName).Infof("VM started Pid:%d", cmd.Process.Pid)

	tapDevice, err := s.fountain.CreateTapDevice(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create tap device: %w", err)
	}
	cleanup.Add(func() {
		if err := s.fountain.DestroyTapDevice(tapDevice); err != nil {
			log.WithError(err).Errorf("failed to delete tap device: %s", tapDevice)
		}
	})

	guestIP, err := s.ipAllocator.AllocateIP()
	if err != nil {
		return nil, fmt.Errorf("error allocating guest ip: %w", err)
	}
	log.Infof("Allocated IP: %v", guestIP)
	cleanup.Add(func() {
		log.WithFields(log.Fields{"vmname": vmName, "action": "cleanup", "api": "createVM", "ip": guestIP.String()}).Info("freeing IP")
		s.ipAllocator.FreeIP(guestIP.IP)
	})

	vsockPath := path.Join(vmStateDir, "vsock.sock")
	cid, err := s.cidAllocator.AllocateCID()
	if err != nil {
		return nil, fmt.Errorf("failed to allocate CID: %w", err)
	}
	cleanup.Add(func() {
		if err := s.cidAllocator.FreeCID(cid); err != nil {
			log.WithError(err).Errorf("failed to free CID: %d", cid)
		}
	})

	statefulDiskPath := path.Join(vmStateDir, statefulDiskFilename)
	err = createStatefulDisk(statefulDiskPath, s.config.StatefulSizeInMB)
	if err != nil {
		return nil, fmt.Errorf("failed to create stateful disk: %w", err)
	}
	cleanup.Add(func() {
		if err := os.Remove(statefulDiskPath); err != nil {
			log.WithError(err).Errorf("failed to remove stateful disk: %s", statefulDiskPath)
		}
	})

	vcpus := calculateVCPUCount()
	numBlockDeviceQueues := vcpus
	memorySizeMB, err := calculateGuestMemorySizeInMB(s.config.GuestMemPercentage)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate guest memory size: %w", err)
	}
	log.Infof("Calculated vCPUs: %d, memory size: %d MB", vcpus, memorySizeMB)

	vmConfig := chvapi.VmConfig{
		Payload: chvapi.PayloadConfig{
			Kernel:    String(kernelPath),
			Cmdline:   String(getKernelCmdLine(s.config.BridgeIP, guestIP.String(), vmName)),
			Initramfs: String(initramfsPath),
		},
		Disks: []chvapi.DiskConfig{
			{Path: rootfsPath, Readonly: Bool(true), NumQueues: &numBlockDeviceQueues},
			{Path: statefulDiskPath, NumQueues: &numBlockDeviceQueues},
		},
		Cpus:    &chvapi.CpusConfig{BootVcpus: vcpus, MaxVcpus: vcpus},
		Memory:  &chvapi.MemoryConfig{Size: int64(memorySizeMB) * 1024 * 1024},
		Serial:  chvapi.NewConsoleConfig(serialPortMode),
		Console: chvapi.NewConsoleConfig(consolePortMode),
		Net: []chvapi.NetConfig{
			{Tap: String(tapDevice.Name), NumQueues: Int32(numNetDeviceQueues), QueueSize: Int32(netDeviceQueueSizeBytes), Id: String(netDeviceId)},
		},
		Vsock: &chvapi.VsockConfig{Cid: int64(cid), Socket: vsockPath},
	}

	log.Info("Calling CreateVM")
	req := apiClient.DefaultAPI.CreateVM(ctx)
	req = req.VmConfig(vmConfig)

	resp, err := req.Execute()
	if err != nil {
		log.Errorf("CreateVM API call failed with error: %v", err)
		if resp != nil {
			log.Errorf("Response Status Code: %d", resp.StatusCode)
			if resp.Body != nil {
				body, readErr := io.ReadAll(resp.Body)
				if readErr != nil {
					log.Errorf("Failed to read response body: %v", readErr)
				} else {
					log.Errorf("Response Body: %s", string(body))
				}
			}
		}
		return nil, fmt.Errorf("failed to start VM: %w", err)
	}
	if resp.StatusCode != 204 {
		log.Errorf("CreateVM returned unexpected status: %d", resp.StatusCode)
		if resp.Body != nil {
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				log.Errorf("Failed to read response body: %v", readErr)
			} else {
				log.Errorf("Response Body: %s", string(body))
			}
		}
		return nil, fmt.Errorf("failed to start VM. bad status: %v", resp)
	}

	newVM := &vm{
		name:             vmName,
		stateDirPath:     vmStateDir,
		apiSocketPath:    apiSocketPath,
		apiClient:        apiClient,
		process:          cmd.Process,
		ip:               guestIP,
		tapDevice:        tapDevice,
		status:           vmStatusRunning,
		vsockPath:        vsockPath,
		cid:              cid,
		statefulDiskPath: statefulDiskPath,
	}
	log.Infof("Successfully created VM: %s", vmName)

	s.lock.Lock()
	s.vms[vmName] = newVM
	s.lock.Unlock()

	cleanup.Release()
	return newVM, nil
}

func (v *vm) boot(ctx context.Context) error {
	v.lock.Lock()
	defer v.lock.Unlock()

	resp, err := v.apiClient.DefaultAPI.BootVM(ctx).Execute()
	if err != nil {
		return fmt.Errorf("failed to boot VM resp.Body: %v: %w", resp.Body, err)
	}
	if resp.StatusCode != 204 {
		return fmt.Errorf("failed to boot VM. bad status: %v", resp)
	}

	log.Infof("Successfully booted VM: %s", v.name)
	v.status = vmStatusRunning
	return nil
}

func (v *vm) destroy(ctx context.Context) error {
	v.lock.Lock()
	defer v.lock.Unlock()

	logger := log.WithField("vmName", v.name)

	shutdownReq := v.apiClient.DefaultAPI.ShutdownVM(ctx)
	resp, err := shutdownReq.Execute()
	if err != nil {
		logger.Warnf("failed to shutdown VM before deleting: %v", err)
	} else if resp.StatusCode >= 300 {
		logger.Warnf("failed to shutdown VM before deleting. bad status: %v", resp)
	}

	deleteReq := v.apiClient.DefaultAPI.DeleteVM(ctx)
	resp, err = deleteReq.Execute()
	if err != nil {
		return status.Error(codes.Internal, fmt.Sprintf("failed to delete VM: %v", err))
	}

	if resp.StatusCode >= 300 {
		return status.Error(codes.Internal, fmt.Sprintf("failed to stop VM. bad status: %v", resp))
	}

	shutdownVMMReq := v.apiClient.DefaultAPI.ShutdownVMM(ctx)
	resp, err = shutdownVMMReq.Execute()
	if err != nil {
		return status.Error(codes.Internal, fmt.Sprintf("failed to shutdown VMM: %v", err))
	}

	if resp.StatusCode >= 300 {
		return status.Error(codes.Internal, fmt.Sprintf("failed to shutdown VMM. bad status: %v", resp))
	}

	err = reapProcess(v.process, logger, reapVmTimeout)
	if err != nil {
		logger.Warnf("failed to reap VM process: %v", err)
	}

	log.Infof("Deleting iptables rules for IP: %s", v.ip.String())
	err = cleanupAllIPTablesRulesForIP(v.ip.IP.String())
	if err != nil {
		logger.Warnf("failed to delete iptables rules: %v", err)
	}

	err = os.RemoveAll(v.stateDirPath)
	if err != nil {
		log.Warnf("Failed to delete directory %s: %v", v.stateDirPath, err)
	}
	return nil
}

func (s *Server) destroyVM(ctx context.Context, vmName string) error {
	logger := log.WithField("vmName", vmName)
	logger.Infof("received request to destroy VM")
	vm := s.getVMAtomic(vmName)
	if vm == nil {
		return fmt.Errorf("vm %s not found", vmName)
	}

	err := vm.destroy(ctx)
	if err != nil {
		return fmt.Errorf("failed to destroy vm: %s: %w", vmName, err)
	}

	err = s.fountain.DestroyTapDevice(vm.tapDevice)
	if err != nil {
		return fmt.Errorf("failed to destroy the tap device for vm: %s: %w", vmName, err)
	}

	err = s.ipAllocator.FreeIP(vm.ip.IP)
	if err != nil {
		return fmt.Errorf("failed to free IP: %s: %w", vm.ip.String(), err)
	}

	err = s.cidAllocator.FreeCID(vm.cid)
	if err != nil {
		log.WithError(err).Errorf("failed to free CID: %d", vm.cid)
	}

	s.lock.Lock()
	delete(s.vms, vmName)
	s.lock.Unlock()
	return nil
}

// StartVM starts a new VM or boots an existing one.
func (s *Server) StartVM(ctx context.Context, req *serverapi.StartVMRequest) (*serverapi.StartVMResponse, error) {
	vmName := req.GetVmName()
	if vmName == "" {
		return nil, fmt.Errorf("vmName is required")
	}
	logger := log.WithField("vmName", vmName)

	kernelPath := req.GetKernel()
	rootfsPath := req.GetRootfs()
	initramfsPath := req.GetInitramfs()
	logger.Infof("Starting VM")

	if kernelPath == "" {
		kernelPath = s.config.KernelPath
	}
	if rootfsPath == "" {
		rootfsPath = s.config.RootfsPath
	}
	if initramfsPath == "" {
		initramfsPath = s.config.InitramfsPath
	}

	vm := s.getVMAtomic(vmName)
	if vm != nil {
		err := vm.boot(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to boot existing VM: %v", err)
		}
	} else {
		cleanup := cleanup.Make(func() {
			logger.Info("start VM clean up done")
		})
		defer func() {
			cleanup.Clean()
		}()

		var err error
		vm, err = s.createVM(ctx, vmName, kernelPath, initramfsPath, rootfsPath)
		if err != nil {
			logger.Errorf("failed to create VM: %v", err)
			return nil, err
		}

		cleanup.Add(func() {
			logger.Info("shutting down VM")
			resp, err := vm.apiClient.DefaultAPI.ShutdownVM(ctx).Execute()
			if err != nil {
				logger.WithError(err).Errorf("failed to shutdown VM: %v", err)
			}
			if resp.StatusCode != 204 {
				logger.WithError(err).Errorf("failed to shutdown VM. bad status: %v", resp)
			}
		})

		err = vm.boot(ctx)
		if err != nil {
			logger.Errorf("failed to boot VM: %v", err)
			return nil, err
		}
		cleanup.Release()
	}

	logger.WithField("vmIP", vm.ip.IP.String()).Infof("Waiting for cmd server to be ready")
	err := waitForCmdServerReady(ctx, vm.ip.IP.String())
	if err != nil {
		logger.WithError(err).Warnf("command server not ready")
	}
	logger.Infof("VM ready")

	return &serverapi.StartVMResponse{
		VmName:        serverapi.PtrString(vmName),
		Ip:            serverapi.PtrString(vm.ip.String()),
		Status:        serverapi.PtrString(vm.status.String()),
		TapDeviceName: serverapi.PtrString(vm.tapDevice.Name),
	}, nil
}

// DestroyVM destroys a specific VM.
func (s *Server) DestroyVM(ctx context.Context, vmName string) (*serverapi.VMResponse, error) {
	err := s.destroyVM(ctx, vmName)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to destroy vm: %s: %v", vmName, err)
	}

	return &serverapi.VMResponse{
		Success: serverapi.PtrBool(true),
	}, nil
}

// DestroyAllVMs destroys all running VMs.
func (s *Server) DestroyAllVMs(ctx context.Context) (*serverapi.DestroyAllVMsResponse, error) {
	log.Infof("received request to destroy all VMs")

	s.lock.RLock()
	vmNames := make([]string, 0, len(s.vms))
	for name := range s.vms {
		vmNames = append(vmNames, name)
	}
	s.lock.RUnlock()

	var finalErr error
	for _, vmName := range vmNames {
		err := s.destroyVM(ctx, vmName)
		if err != nil {
			log.Warnf("failed to destroy and clean up vm: %s", vmName)
		}
		finalErr = errors.Join(finalErr, err)
	}

	if finalErr != nil {
		return nil, status.Errorf(codes.Internal, "failed to destroy all VMs: %v", finalErr)
	}

	return &serverapi.DestroyAllVMsResponse{
		Success: serverapi.PtrBool(true),
	}, nil
}

// ListAllVMs returns information about all VMs.
func (s *Server) ListAllVMs(ctx context.Context) (*serverapi.ListAllVMsResponse, error) {
	resp := &serverapi.ListAllVMsResponse{}
	var vms []serverapi.ListAllVMsResponseVmsInner

	s.lock.RLock()
	defer s.lock.RUnlock()

	for _, vm := range s.vms {
		var ipString string
		if vm.ip != nil {
			ipString = vm.ip.String()
		}

		vmInfo := serverapi.ListAllVMsResponseVmsInner{
			VmName:        serverapi.PtrString(vm.name),
			Ip:            serverapi.PtrString(ipString),
			Status:        serverapi.PtrString(vm.status.String()),
			TapDeviceName: serverapi.PtrString(vm.tapDevice.Name),
		}
		vms = append(vms, vmInfo)
	}
	resp.Vms = vms
	return resp, nil
}

// ListVM returns information about a specific VM.
func (s *Server) ListVM(ctx context.Context, vmName string) (*serverapi.ListVMResponse, error) {
	vm := s.getVMAtomic(vmName)
	if vm == nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("vm not found: %s", vmName))
	}

	var ipString string
	if vm.ip != nil {
		ipString = vm.ip.String()
	}

	return &serverapi.ListVMResponse{
		VmName:        serverapi.PtrString(vm.name),
		Ip:            serverapi.PtrString(ipString),
		Status:        serverapi.PtrString(vm.status.String()),
		TapDeviceName: serverapi.PtrString(vm.tapDevice.Name),
	}, nil
}

// VMExec executes a command in a VM.
func (s *Server) VMExec(ctx context.Context, vmName string, cmd string, blocking bool) (*serverapi.VmExecResponse, error) {
	vm := s.getVMAtomic(vmName)
	if vm == nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("vm not found: %s", vmName))
	}

	url := fmt.Sprintf("http://%s:4031", vm.ip.IP.String())
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	return vm.handleExec(ctx, client, url, cmd, blocking)
}

func (v *vm) handleExec(ctx context.Context, client *http.Client, baseURL string, cmd string, blocking bool) (*serverapi.VmExecResponse, error) {
	reqBody := struct {
		Cmd      string `json:"cmd"`
		Blocking bool   `json:"blocking"`
	}{
		Cmd:      cmd,
		Blocking: blocking,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/cmd", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status: %d", resp.StatusCode)
	}

	var cmdResp cmdserver.RunCmdResponse
	if err := json.NewDecoder(resp.Body).Decode(&cmdResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &serverapi.VmExecResponse{
		Output: serverapi.PtrString(cmdResp.Output),
		Error:  serverapi.PtrString(cmdResp.Error),
	}, nil
}

// waitForCmdServerReady waits for the command server in the VM to be ready.
func waitForCmdServerReady(ctx context.Context, vmIP string) error {
	url := fmt.Sprintf("http://%s:4031/", vmIP)
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	deadline := time.Now().Add(cmdServerReadyTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			resp, err := client.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
			time.Sleep(cmdServerReadyRetryDelay)
		}
	}
	return fmt.Errorf("timeout waiting for cmd server to be ready")
}
