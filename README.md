# CBox - Lightweight MicroVM Execution Environment

CBox is a lightweight MicroVM management system built on Cloud Hypervisor, designed specifically for executing commands in isolated sandbox environments with callback support.

## Overview

CBox is a stripped-down version of the Arrakis project, focusing only on:
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

## Components

### Host-side
- **cbox-restserver**: REST API server for VM lifecycle management and command execution
- **cbox-rootfsmaker**: Tool to build guest rootfs images from Dockerfiles

### Guest-side
- **cbox-guestinit**: Initializes networking inside the guest VM
- **cbox-cmdserver**: HTTP server that accepts and executes commands
- **cbox-vsockserver**: Handles vsock-based communication for callbacks

## Prerequisites

- Linux host with KVM support
- Go 1.23+
- Docker (for building guest images)
- OpenAPI Generator CLI (for API code generation)
- Root/sudo access (for networking setup)

Required binaries in `resources/bin/`:
- `cloud-hypervisor` - Cloud Hypervisor VMM
- `vmlinux.bin` - Linux kernel image
- `busybox` - For initramfs

## Building

```bash
# Build everything
make all

# Build specific components
make restserver    # Host REST server
make guestinit     # Guest init binary
make cmdserver     # Guest command server
make vsockserver   # Guest vsock server
make guestrootfs   # Guest root filesystem (requires sudo)
```

## Configuration

Edit `config.yaml` to configure the server:

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

## Usage

### Starting the Server

```bash
sudo ./out/cbox-restserver --config ./config.yaml
```

### API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/v1/vms` | Start a new VM |
| GET | `/v1/vms` | List all VMs |
| GET | `/v1/vms/{name}` | Get VM details |
| DELETE | `/v1/vms/{name}` | Destroy a VM |
| DELETE | `/v1/vms` | Destroy all VMs |
| POST | `/v1/vms/{name}/exec` | Execute a command in VM |
| GET | `/v1/health` | Health check |

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

## Development

### Project Structure

```
ci-box/
├── api/                    # OpenAPI specifications
├── cmd/
│   ├── cmdserver/          # Guest command server
│   ├── guestinit/          # Guest initialization
│   ├── restserver/         # Host REST server
│   ├── rootfsmaker/        # Rootfs builder
│   └── vsockserver/        # Guest vsock server
├── initramfs/              # Initramfs build scripts
├── pkg/
│   ├── callback/           # Callback session management
│   ├── cmdserver/          # Shared types
│   ├── config/             # Configuration loading
│   └── server/             # VM server implementation
│       ├── cidallocator/   # CID allocation
│       ├── fountain/       # Tap device management
│       └── ipallocator/    # IP allocation
├── resources/
│   ├── bin/                # External binaries (not in git)
│   └── scripts/
│       ├── guest/          # Guest callback scripts
│       └── rootfs/         # Dockerfile for guest image
├── config.yaml             # Server configuration
├── go.mod
├── go.sum
└── Makefile
```

### Regenerating API Code

```bash
make serverapi
make chvapi
```

## CI/CD

CBox uses GitHub Actions for continuous integration and delivery. The workflow is defined in `.github/workflows/build-binaries.yml`.

### What the CI Pipeline Does

1. **Sets up Go 1.23.1** and OpenAPI Generator CLI
2. **Downloads required binaries** (busybox for initramfs)
3. **Generates API clients** from OpenAPI specs
4. **Builds all Go binaries** (restserver, guestinit, cmdserver, vsockserver, rootfsmaker)
5. **Builds the guest rootfs image** using Docker
6. **Compresses artifacts** for efficient storage
7. **Uploads build artifacts** (retained for 7 days)
8. **Creates a GitHub Release** (on push to main branch)

### Triggering Builds

Builds are automatically triggered on:
- Push to `main` or `ci-cd-test` branches
- Pull requests targeting `main`

### Release Artifacts

Each release includes:
- `cbox-restserver` - Host REST API server
- `cbox-guestinit` - Guest initialization binary
- `cbox-cmdserver` - Guest command server
- `cbox-vsockserver` - Guest vsock server
- `cbox-rootfsmaker` - Rootfs builder tool
- `initramfs.cpio.gz` - Initramfs image
- `cbox-guestrootfs-ext4.img.tar.gz` - Compressed guest rootfs
- `config.yaml` - Default configuration
- `VERSION` - Build metadata

### Manual Build

To build locally without CI:

```bash
# Ensure you have the prerequisites
make all
```

## License

Same license as the parent Arrakis project.