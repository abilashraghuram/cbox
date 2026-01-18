# CBox Setup Changelog

All notable changes to the CBox setup scripts and deployment process will be documented in this file.

## [1.0.0] - 2025-01-18

### Initial Release

CBox is a lightweight MicroVM execution environment forked from Arrakis, focused specifically on command execution and callback support.

### Added

- `setup.sh` - Main setup script to download and install CBox prebuilt binaries
  - Downloads cbox-restserver binary
  - Downloads and extracts guest rootfs image
  - Downloads initramfs
  - Downloads config.yaml
  - Downloads and runs install-images.py for VM binaries
  - Displays version information after installation

- `install-deps.sh` - Development environment setup script
  - Installs make and build essentials
  - Installs nvm and Node.js
  - Installs OpenAPI Generator CLI
  - Installs Go 1.23.6
  - Installs OpenJDK (required by OpenAPI generator)
  - Installs Docker (for building guest rootfs)
  - Installs Python3 and pip

- `install-images.py` - VM binary image downloader
  - Downloads Linux kernel (vmlinux.bin)
  - Downloads Cloud Hypervisor VMM
  - Downloads BusyBox for initramfs

- `gcp-instructions.md` - GCP deployment guide
  - VM creation with nested virtualization
  - Firewall configuration
  - Quick install instructions
  - API reference
  - Troubleshooting guide

### Features

- **Lightweight**: Stripped down from Arrakis, focusing only on exec and callbacks
- **Simple API**: REST API for VM lifecycle and command execution
- **Callback Support**: VMs can call back to the host via HTTP
- **GCP Ready**: Tested on Google Compute Engine with nested virtualization

### Removed (compared to Arrakis)

- Snapshotting and restore functionality
- File upload/download endpoints
- Port forwarding configuration
- VNC support
- Client binary (use curl or any HTTP client)

### Requirements

- Ubuntu 22.04 (or compatible Linux distribution)
- Nested virtualization support (for cloud VMs)
- Root/sudo access for networking setup
- 4GB+ RAM recommended
- 50GB+ disk space

### Quick Start

```bash
# On a fresh Ubuntu 22.04 VM with nested virtualization
cd $HOME
curl -sSL "https://raw.githubusercontent.com/abilashraghuram/cbox/main/setup/setup.sh" | bash
cd cbox-prebuilt
sudo ./cbox-restserver
```

### Known Issues

- None reported yet

### Future Plans

- Alpine Linux-based lightweight guest image option
- Reduced memory footprint configuration
- Session-based VM reuse
- Performance benchmarking tools