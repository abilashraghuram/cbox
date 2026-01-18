# CBox Setup Instructions on GCP

## Overview

This guide walks you through setting up CBox on a Google Compute Engine (GCE) virtual machine with nested virtualization enabled. CBox is a lightweight MicroVM execution environment for running isolated sandboxes with exec and callback support.

## Prerequisites

- Google Cloud Platform account
- `gcloud` CLI installed and configured locally

## Step 1: Create a GCE VM with Nested Virtualization

Run the following commands to create a VM with nested virtualization support:

```bash
# Set your variables
VM_NAME=cbox-vm
PROJECT_ID=<your-project-id>
SERVICE_ACCOUNT=<your-service-account>
ZONE=us-west1-b

# Create VM instance with nested virtualization enabled
gcloud compute instances create ${VM_NAME} \
  --project=${PROJECT_ID} \
  --zone=${ZONE} \
  --machine-type=n1-standard-4 \
  --network-interface=network-tier=STANDARD,stack-type=IPV4_ONLY,subnet=default \
  --maintenance-policy=MIGRATE \
  --provisioning-model=STANDARD \
  --service-account=${SERVICE_ACCOUNT} \
  --scopes=https://www.googleapis.com/auth/devstorage.read_only,https://www.googleapis.com/auth/logging.write,https://www.googleapis.com/auth/monitoring.write,https://www.googleapis.com/auth/service.management.readonly,https://www.googleapis.com/auth/servicecontrol,https://www.googleapis.com/auth/trace.append \
  --create-disk=auto-delete=yes,boot=yes,device-name=${VM_NAME}-disk,image=projects/ubuntu-os-cloud/global/images/ubuntu-2204-jammy-v20250128,mode=rw,size=50,type=pd-standard \
  --no-shielded-secure-boot \
  --shielded-vtpm \
  --shielded-integrity-monitoring \
  --enable-nested-virtualization

# Add network tags and create firewall rule for API access
NETWORK_TAG=allow-cbox-ports
FIREWALL_RULE=allow-cbox-ports-rule

gcloud compute instances add-tags ${VM_NAME} \
  --tags=${NETWORK_TAG} \
  --zone=${ZONE}

gcloud compute firewall-rules create ${FIREWALL_RULE} \
  --direction=INGRESS \
  --priority=1000 \
  --network=default \
  --action=ALLOW \
  --rules=tcp:7000 \
  --source-ranges=0.0.0.0/0 \
  --target-tags=${NETWORK_TAG} \
  --description="Allow TCP ingress on port 7000 for CBox REST API"
```

### Machine Type Recommendations

| Use Case | Machine Type | vCPUs | Memory | Notes |
|----------|--------------|-------|--------|-------|
| Testing | n1-standard-2 | 2 | 7.5 GB | Minimum for single VM |
| Development | n1-standard-4 | 4 | 15 GB | Recommended for dev |
| Production | n1-standard-8 | 8 | 30 GB | Multiple concurrent VMs |

## Step 2: SSH into the VM

```bash
gcloud compute ssh ${VM_NAME} --zone=${ZONE}
```

## Step 3: Install CBox (Quick Install)

Use the setup script to install CBox with prebuilt binaries:

```bash
cd $HOME
curl -sSL "https://raw.githubusercontent.com/abilashraghuram/cbox/main/setup/setup.sh" | bash
```

This will:
- Download the latest CBox release binaries
- Download the guest rootfs image
- Download required VM images (kernel, cloud-hypervisor, busybox)
- Set up the directory structure

## Step 4: Verify the Installation

```bash
cd $HOME/cbox-prebuilt
ls -la
```

You should see:
```
cbox-restserver          # REST API server binary
config.yaml              # Configuration file
VERSION                  # Version information
resources/bin/           # VM binaries (kernel, cloud-hypervisor, busybox)
out/                     # Guest rootfs and initramfs
```

## Step 5: Run the CBox REST Server

```bash
cd $HOME/cbox-prebuilt
sudo ./cbox-restserver
```

The server will start on port 7000 by default.

## Step 6: Test the Installation

Open a new SSH session and run:

```bash
# Check server health
curl http://localhost:7000/v1/health

# Create a sandbox VM
curl -X POST http://localhost:7000/v1/vms \
  -H "Content-Type: application/json" \
  -d '{"vmName": "test-sandbox"}'

# Execute a command in the sandbox
curl -X POST http://localhost:7000/v1/vms/test-sandbox/exec \
  -H "Content-Type: application/json" \
  -d '{"cmd": "echo Hello from CBox!", "blocking": true}'

# List all VMs
curl http://localhost:7000/v1/vms

# Destroy the test VM
curl -X DELETE http://localhost:7000/v1/vms/test-sandbox
```

## Step 7: Access from External IP (Optional)

To access the CBox API from outside the VM:

```bash
# Get your VM's external IP
EXTERNAL_IP=$(curl -s ifconfig.me)
echo "CBox API available at: http://${EXTERNAL_IP}:7000"

# From your local machine
curl http://<EXTERNAL-IP>:7000/v1/health
```

## API Reference

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/health` | Health check |
| POST | `/v1/vms` | Create a new VM |
| GET | `/v1/vms` | List all VMs |
| GET | `/v1/vms/{name}` | Get VM details |
| DELETE | `/v1/vms/{name}` | Destroy a VM |
| DELETE | `/v1/vms` | Destroy all VMs |
| POST | `/v1/vms/{name}/exec` | Execute command in VM |

### Create VM with Callback URL

```bash
curl -X POST http://localhost:7000/v1/vms \
  -H "Content-Type: application/json" \
  -d '{
    "vmName": "my-sandbox",
    "callbackUrl": "http://your-service:8080/callback"
  }'
```

### Execute Command

```bash
# Blocking (wait for result)
curl -X POST http://localhost:7000/v1/vms/my-sandbox/exec \
  -H "Content-Type: application/json" \
  -d '{"cmd": "ls -la /", "blocking": true}'

# Non-blocking (fire and forget)
curl -X POST http://localhost:7000/v1/vms/my-sandbox/exec \
  -H "Content-Type: application/json" \
  -d '{"cmd": "sleep 10 && echo done", "blocking": false}'
```

## Configuration

The default `config.yaml` can be customized:

```yaml
hostservices:
  restserver:
    host: "0.0.0.0"
    port: "7000"
    state_dir: "./vm-state"
    bridge_name: "br0"
    bridge_ip: "10.20.1.1/24"
    bridge_subnet: "10.20.1.0/24"
    chv_bin: "./resources/bin/cloud-hypervisor"
    kernel: "./resources/bin/vmlinux.bin"
    rootfs: "./out/cbox-guestrootfs-ext4.img"
    initramfs: "./out/initramfs.cpio.gz"
    stateful_size_in_mb: "2048"      # Per-VM writable storage
    guest_mem_percentage: "30"        # % of host RAM per VM
```

## Troubleshooting

### Server Won't Start

```bash
# Check if port 7000 is already in use
sudo netstat -tlnp | grep 7000

# Check for errors in the output
sudo ./cbox-restserver 2>&1 | head -50

# Verify nested virtualization is enabled
cat /sys/module/kvm_intel/parameters/nested
# Should output: Y
```

### VM Creation Fails

```bash
# Check KVM support
ls -la /dev/kvm

# Ensure current user can access KVM (or run as root)
sudo chmod 666 /dev/kvm

# Check bridge setup
ip link show br0
ip addr show br0
```

### Network Issues

```bash
# Verify bridge is configured
ip link show br0

# Check IP forwarding
cat /proc/sys/net/ipv4/ip_forward
# Should be 1

# Enable if needed
sudo sysctl -w net.ipv4.ip_forward=1

# Check iptables NAT rules
sudo iptables -t nat -L POSTROUTING -n
```

## Building from Source (Optional)

If you want to build CBox from source instead of using prebuilt binaries:

```bash
# Install development dependencies
curl -sSL "https://raw.githubusercontent.com/abilashraghuram/cbox/main/setup/install-deps.sh" | bash

# Clone the repository
git clone https://github.com/abilashraghuram/cbox.git
cd cbox

# Download required binary images
./setup/install-images.py

# Build everything
make all

# Run the server
sudo ./out/cbox-restserver
```

## Cleanup

To remove the VM and firewall rules:

```bash
# Delete the VM
gcloud compute instances delete ${VM_NAME} --zone=${ZONE}

# Delete the firewall rule
gcloud compute firewall-rules delete ${FIREWALL_RULE}
```

## Support

For issues and feature requests, please open an issue on GitHub:
https://github.com/abilashraghuram/cbox/issues