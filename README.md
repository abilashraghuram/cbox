# CBox - Lightweight MicroVM Execution Environment

CBox is a lightweight MicroVM management system built on Cloud Hypervisor, designed specifically for executing commands in isolated sandbox environments with callback support.

## Overview

CBox is a stripped-down fork of the Arrakis project, focusing only on:
- **Command Execution**: Execute commands inside isolated VMs
- **Callback Support**: Allow guest VMs to call back to the host for RPC-style communication

Features intentionally **excluded** to keep things lightweight:
- Snapshotting and restore
- File read/write operations
- Port forwarding
- VNC support

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Host System                              │
│  ┌─────────────────┐    ┌─────────────────────────────────────┐ │
│  │ cbox-restserver │◄───│  External Client (with callback URL) │ │
│  │    (port 7000)  │    └─────────────────────────────────────┘ │
│  └────────┬────────┘                                            │
│           │                                                      │
│           ▼                                                      │
│  ┌─────────────────┐                                            │
│  │ Cloud Hypervisor│                                            │
│  │      (VMM)      │                                            │
│  └────────┬────────┘                                            │
│           │                                                      │
├───────────┼──────────────────────────────────────────────────────┤
│           │              Guest VM                                │
│           ▼                                                      │
│  ┌─────────────────┐    ┌─────────────────┐                     │
│  │ cbox-cmdserver  │    │ cbox-vsockserver│                     │
│  │   (port 4031)   │    │   (port 4032)   │                     │
│  └─────────────────┘    └─────────────────┘                     │
└─────────────────────────────────────────────────────────────────┘
```

## Quick Start (GCP Deployment)

### Step 1: Create a GCE VM with Nested Virtualization

```bash
VM_NAME=cbox-vm
PROJECT_ID=<your-project-id>
SERVICE_ACCOUNT=<your-service-account>
ZONE=us-west1-b

gcloud compute instances create ${VM_NAME} \
  --project=${PROJECT_ID} \
  --zone=${ZONE} \
  --machine-type=n1-standard-4 \
  --network-interface=network-tier=STANDARD,stack-type=IPV4_ONLY,subnet=default \
  --create-disk=auto-delete=yes,boot=yes,device-name=${VM_NAME}-disk,image=projects/ubuntu-os-cloud/global/images/ubuntu-2204-jammy-v20250128,mode=rw,size=50,type=pd-standard \
  --enable-nested-virtualization

# Create firewall rule for API access
gcloud compute firewall-rules create allow-cbox-api \
  --direction=INGRESS \
  --action=ALLOW \
  --rules=tcp:7000 \
  --source-ranges=0.0.0.0/0
```

### Step 2: SSH into the VM and Install CBox

```bash
gcloud compute ssh ${VM_NAME} --zone=${ZONE}

# Install CBox using the setup script
cd $HOME
curl -sSL "https://raw.githubusercontent.com/abilashraghuram/cbox/main/setup/setup.sh" | bash
```

### Step 3: Run CBox

```bash
cd $HOME/cbox-prebuilt
sudo ./cbox-restserver
```

### Step 4: Test It

```bash
# Health check
curl http://localhost:7000/v1/health

# Create a VM
curl -X POST http://localhost:7000/v1/vms \
  -H "Content-Type: application/json" \
  -d '{"vmName": "test-sandbox"}'

# Execute a command
curl -X POST http://localhost:7000/v1/vms/test-sandbox/exec \
  -H "Content-Type: application/json" \
  -d '{"cmd": "echo Hello from CBox!", "blocking": true}'

# Destroy the VM
curl -X DELETE http://localhost:7000/v1/vms/test-sandbox
```

## Components

### Host-side
- **cbox-restserver**: REST API server for VM lifecycle management and command execution
- **cbox-rootfsmaker**: Tool to build guest rootfs images from Dockerfiles

### Guest-side
- **cbox-guestinit**: Initializes networking inside the guest VM
- **cbox-cmdserver**: HTTP server that accepts and executes commands
- **cbox-vsockserver**: Handles vsock-based communication for callbacks

## API Reference

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/health` | Health check |
| POST | `/v1/vms` | Start a new VM |
| GET | `/v1/vms` | List all VMs |
| GET | `/v1/vms/{name}` | Get VM details |
| DELETE | `/v1/vms/{name}` | Destroy a VM |
| DELETE | `/v1/vms` | Destroy all VMs |
| POST | `/v1/vms/{name}/exec` | Execute command in VM |

### Starting a VM with Callback Support

```bash
curl -X POST http://localhost:7000/v1/vms \
  -H "Content-Type: application/json" \
  -d '{
    "vmName": "my-sandbox",
    "callbackUrl": "http://my-service:8080/callback"
  }'
```

### Executing Commands

```bash
curl -X POST http://localhost:7000/v1/vms/my-sandbox/exec \
  -H "Content-Type: application/json" \
  -d '{
    "cmd": "echo hello world",
    "blocking": true
  }'
```

### Using Callbacks from Guest

Inside the guest VM, you can use the callback library:

**Python:**
```python
from cbox_callback import callback

# Make a callback to the host
result = callback("my_method", {"param1": "value1"})
print(result)
```

**Shell:**
```bash
cbox_callback my_method '{"param1": "value1"}'
```

## Resource Allocation

Each MicroVM receives the following resources:

| Resource | Allocation | Notes |
|----------|------------|-------|
| **vCPUs** | host_cpus / 2 | Min 1, Max 8 |
| **RAM** | 30% of host RAM | Min 1GB, Max 32GB |
| **Rootfs** | 4 GB (shared) | Read-only |
| **Stateful Disk** | 2 GB | Per-VM writable overlay |
| **IP Address** | 1 from 10.20.1.x pool | ~253 VMs max |

## Configuration

Edit `config.yaml` to customize:

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
    stateful_size_in_mb: "2048"
    guest_mem_percentage: "30"
```

## Building from Source

### Prerequisites

- Linux host with KVM support
- Go 1.23+
- Docker (for building guest images)
- OpenAPI Generator CLI
- Root/sudo access

### Build Steps

```bash
# Install development dependencies
./setup/install-deps.sh

# Download required binary images (kernel, cloud-hypervisor, busybox)
./setup/install-images.py

# Build everything
make all
```

### Build Specific Components

```bash
make restserver    # Host REST server
make guestinit     # Guest init binary
make cmdserver     # Guest command server
make vsockserver   # Guest vsock server
make guestrootfs   # Guest root filesystem (requires sudo)
```

## Project Structure

```
cbox/
├── .github/workflows/     # CI/CD workflows
├── api/                   # OpenAPI specifications
├── cmd/
│   ├── cmdserver/         # Guest command server
│   ├── guestinit/         # Guest initialization
│   ├── restserver/        # Host REST server
│   ├── rootfsmaker/       # Rootfs builder
│   └── vsockserver/       # Guest vsock server
├── initramfs/             # Initramfs build scripts
├── pkg/
│   ├── callback/          # Callback session management
│   ├── cmdserver/         # Shared types
│   ├── config/            # Configuration loading
│   └── server/            # VM server implementation
├── resources/
│   ├── bin/               # External binaries
│   └── scripts/
│       ├── guest/         # Guest callback scripts
│       └── rootfs/        # Dockerfile for guest image
├── setup/                 # Setup and deployment scripts
│   ├── setup.sh           # Quick install script
│   ├── install-deps.sh    # Dev environment setup
│   ├── install-images.py  # VM binary downloader
│   └── gcp-instructions.md
├── config.yaml
├── go.mod
└── Makefile
```

## CI/CD

CBox uses GitHub Actions for CI/CD. The workflow:

1. Builds all Go binaries
2. Generates API clients from OpenAPI specs
3. Builds the guest rootfs image
4. Creates GitHub releases with all artifacts

Builds are triggered on:
- Push to `main` or `ci-cd-test` branches
- Pull requests to `main`

## Troubleshooting

### Server Won't Start

```bash
# Check if port 7000 is in use
sudo netstat -tlnp | grep 7000

# Verify KVM is available
ls -la /dev/kvm

# Check nested virtualization
cat /sys/module/kvm_intel/parameters/nested
```

### VM Creation Fails

```bash
# Check bridge setup
ip link show br0
ip addr show br0

# Enable IP forwarding
sudo sysctl -w net.ipv4.ip_forward=1
```

## Documentation

- [GCP Setup Instructions](setup/gcp-instructions.md)
- [Setup Changelog](setup/CHANGELOG.md)

## License

Same license as the parent Arrakis project.