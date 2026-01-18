package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/coreos/go-systemd/daemon"
	"github.com/mdlayher/vsock"
	log "github.com/sirupsen/logrus"
)

const (
	// Define a base directory to prevent path traversal.
	baseDir = "/tmp/vsockserver"
	port    = 4032

	// Callback configuration
	callbackTimeout = 30 * time.Second
)

// Global variables set from kernel command line
var (
	gatewayIP string
	vmName    string
)

// CallbackRequest represents an RPC callback request to the host.
type CallbackRequest struct {
	VMName string          `json:"vmName"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// CallbackResponse represents the response from a callback.
type CallbackResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// parseKernelCmdLine parses the kernel command line to extract configuration.
func parseKernelCmdLine() error {
	data, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return fmt.Errorf("failed to read /proc/cmdline: %w", err)
	}

	cmdline := string(data)
	parts := strings.Fields(cmdline)

	for _, part := range parts {
		if strings.HasPrefix(part, "gateway_ip=") {
			gatewayIP = strings.Trim(strings.TrimPrefix(part, "gateway_ip="), "\"")
		}
		if strings.HasPrefix(part, "vm_name=") {
			vmName = strings.Trim(strings.TrimPrefix(part, "vm_name="), "\"")
		}
	}

	if gatewayIP == "" {
		return fmt.Errorf("gateway_ip not found in kernel command line")
	}

	// If vmName is not set, try to get it from hostname
	if vmName == "" {
		hostname, err := os.Hostname()
		if err == nil && hostname != "" {
			vmName = hostname
		} else {
			vmName = "unknown"
		}
	}

	log.WithFields(log.Fields{
		"gatewayIP": gatewayIP,
		"vmName":    vmName,
	}).Info("Parsed kernel command line")

	return nil
}

// handleCallback processes a CALLBACK command and sends it to the cbox-restserver.
// The restserver is responsible for routing the callback to the registered HTTP callback URL.
func handleCallback(method string, paramsJSON string) (string, error) {
	// Always send callbacks to the cbox-restserver via the gateway
	hostIP := gatewayIP
	if idx := strings.Index(hostIP, "/"); idx != -1 {
		hostIP = hostIP[:idx]
	}
	url := fmt.Sprintf("http://%s:7000/v1/internal/callback", hostIP)

	// Build the callback request
	req := CallbackRequest{
		VMName: vmName,
		Method: method,
	}

	// Parse params if provided
	if paramsJSON != "" {
		req.Params = json.RawMessage(paramsJSON)
	}

	// Serialize the request
	reqBody, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal callback request: %w", err)
	}

	// Make HTTP request
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: callbackTimeout,
	}

	log.WithFields(log.Fields{
		"url":    url,
		"method": method,
		"vmName": vmName,
	}).Info("Sending callback to cbox-restserver")

	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("callback HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read callback response: %w", err)
	}

	// Check for HTTP errors
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("callback returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse the response
	var callbackResp CallbackResponse
	if err := json.Unmarshal(respBody, &callbackResp); err != nil {
		// If we can't parse as CallbackResponse, return the raw body
		return string(respBody), nil
	}

	if callbackResp.Error != "" {
		return "", fmt.Errorf("callback error: %s", callbackResp.Error)
	}

	// Return the result as a string
	if callbackResp.Result != nil {
		return string(callbackResp.Result), nil
	}
	return "{}", nil
}

// parseCallbackCommand parses a CALLBACK command line.
// Format: CALLBACK <method> [<params_json>]
func parseCallbackCommand(cmd string) (method string, params string, err error) {
	// Remove the "CALLBACK " prefix
	remainder := strings.TrimPrefix(cmd, "CALLBACK ")
	remainder = strings.TrimSpace(remainder)

	if remainder == "" {
		return "", "", fmt.Errorf("CALLBACK command requires a method name")
	}

	// Find the first space to separate method from params
	spaceIdx := strings.Index(remainder, " ")
	if spaceIdx == -1 {
		// No params, just method
		return remainder, "", nil
	}

	method = remainder[:spaceIdx]
	params = strings.TrimSpace(remainder[spaceIdx+1:])

	return method, params, nil
}

func handleConnection(conn *vsock.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	for {
		// Read command from the connection
		cmd, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Errorf("Error reading from connection: %v", err)
			}
			return
		}

		// Trim whitespace and newline
		cmd = strings.TrimSpace(cmd)

		if cmd == "" {
			continue
		}

		// Check if this is a CALLBACK command
		if strings.HasPrefix(cmd, "CALLBACK ") {
			method, params, err := parseCallbackCommand(cmd)
			if err != nil {
				errMsg := fmt.Sprintf("Error: %v\n", err)
				log.WithField("cmd", cmd).WithError(err).Error("Invalid CALLBACK command")
				conn.Write([]byte(errMsg))
				continue
			}

			log.WithFields(log.Fields{
				"method": method,
				"params": params,
			}).Info("Processing CALLBACK command")

			result, err := handleCallback(method, params)
			if err != nil {
				errMsg := fmt.Sprintf("Error: %v\n", err)
				log.WithFields(log.Fields{
					"method": method,
					"error":  err,
				}).Error("CALLBACK failed")
				conn.Write([]byte(errMsg))
				continue
			}

			log.WithFields(log.Fields{
				"method": method,
				"result": result,
			}).Info("CALLBACK completed successfully")

			// Write the result back to the connection
			_, err = conn.Write(append([]byte(result), '\n'))
			if err != nil {
				log.Errorf("Error writing callback response: %v", err)
				return
			}
			continue
		}

		// Regular command execution
		// Set up environment variables with a restricted PATH for security
		env := os.Environ()
		customPath := "/usr/local/bin:/usr/bin:/bin"
		env = append(env, "PATH="+customPath)

		// Create and configure the command
		command := exec.Command("/bin/bash", "-c", cmd)
		command.Env = env
		command.Dir = baseDir

		// Log the command execution
		log.WithFields(log.Fields{
			"cmd":        cmd,
			"workingDir": command.Dir,
		}).Info("Executing command")

		// Execute the command and capture output
		output, err := command.CombinedOutput()
		if err != nil {
			errMsg := fmt.Sprintf("Error: %v\nOutput: %s\n", err, string(output))
			log.WithFields(log.Fields{
				"cmd":    cmd,
				"error":  err,
				"output": string(output),
			}).Error("Command execution failed")
			conn.Write([]byte(errMsg))
			continue
		}

		// Log successful execution
		log.WithFields(log.Fields{
			"cmd":    cmd,
			"output": string(output),
		}).Info("Command executed successfully")

		// Write the output back to the connection
		_, err = conn.Write(append(output, '\n'))
		if err != nil {
			log.Errorf("Error writing response: %v", err)
			return
		}
	}
}

func main() {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		log.Fatalf("Failed to create base directory: %v", err)
	}

	// Parse kernel command line to get gateway IP and VM name
	if err := parseKernelCmdLine(); err != nil {
		log.Warnf("Failed to parse kernel command line: %v", err)
		// Continue anyway, callbacks just won't work
	}

	listener, err := vsock.Listen(uint32(port), &vsock.Config{})
	if err != nil {
		log.Fatalf("Failed to create vsock listener: %v", err)
	}
	defer listener.Close()

	log.Printf("cbox-vsockserver listening on port %d...", port)
	log.Printf("Gateway IP: %s, VM Name: %s", gatewayIP, vmName)

	// Make other services start via systemd since we're ready to debug.
	if _, err := daemon.SdNotify(false, daemon.SdNotifyReady); err != nil {
		log.Warnf("Failed to notify systemd of readiness: %v", err)
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Errorf("Failed to accept connection: %v", err)
			continue
		}

		// Handle each connection in a goroutine
		go handleConnection(conn.(*vsock.Conn))
	}
}
