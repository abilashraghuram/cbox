#!/usr/bin/env bash
set -euo pipefail

# CBox Development Environment Setup Script
# This script installs all dependencies needed to build CBox from source.

# Define colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

print_message() {
  echo -e "${GREEN}[CBox Setup]${NC} $1"
}

print_warning() {
  echo -e "${YELLOW}[Warning]${NC} $1"
}

print_error() {
  echo -e "${RED}[Error]${NC} $1"
}

print_section() {
  echo ""
  echo -e "${GREEN}========================================${NC}"
  echo -e "${GREEN}$1${NC}"
  echo -e "${GREEN}========================================${NC}"
}

# Check if running as root
if [ "$EUID" -eq 0 ]; then
  print_warning "Running as root. Some installations may behave differently."
fi

print_section "CBox Development Environment Setup"
print_message "This script will install the following:"
print_message "  - make (build tool)"
print_message "  - nvm and Node.js (for OpenAPI generator)"
print_message "  - OpenAPI Generator CLI"
print_message "  - Go 1.23.6"
print_message "  - OpenJDK (required by OpenAPI generator)"
print_message "  - Docker (for building guest rootfs)"
echo ""

# Update apt package list
print_section "Updating package list"
sudo apt update

# Install make and essential build tools
print_section "Installing build essentials"
sudo apt install -y make build-essential curl git

# Install nvm using the provided install script
print_section "Installing nvm (Node Version Manager)"
if [ -d "$HOME/.nvm" ]; then
  print_message "nvm already installed, skipping..."
else
  curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.1/install.sh | bash
fi

# Load nvm into the current shell session
export NVM_DIR="$HOME/.nvm"
if [ -s "$NVM_DIR/nvm.sh" ]; then
  . "$NVM_DIR/nvm.sh"
else
  print_error "nvm installation failed or nvm.sh not found."
  exit 1
fi

# Install Node.js using nvm
print_section "Installing Node.js"
nvm install node
nvm use node
print_message "Node.js version: $(node --version)"
print_message "npm version: $(npm --version)"

# Install OpenAPI Generator CLI globally using npm
print_section "Installing OpenAPI Generator CLI"
npm install @openapitools/openapi-generator-cli -g
print_message "OpenAPI Generator installed"

# Install Go programming language
print_section "Installing Go 1.23.6"
GO_VERSION="1.23.6"
GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"

if command -v go &> /dev/null; then
  CURRENT_GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
  print_message "Go is already installed (version: $CURRENT_GO_VERSION)"
  read -p "Do you want to reinstall Go $GO_VERSION? (y/N) " -n 1 -r
  echo
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    sudo rm -rf /usr/local/go
  else
    print_message "Keeping existing Go installation"
    GO_TARBALL=""
  fi
fi

if [ -n "$GO_TARBALL" ]; then
  print_message "Downloading Go $GO_VERSION..."
  curl -LO "https://go.dev/dl/${GO_TARBALL}"
  sudo tar -C /usr/local -xzf "$GO_TARBALL"
  rm "$GO_TARBALL"

  # Add Go to PATH if not already there
  if ! grep -q "/usr/local/go/bin" ~/.bashrc; then
    echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
  fi
  export PATH=$PATH:/usr/local/go/bin
  print_message "Go $GO_VERSION installed successfully"
fi

# Install default JDK (required for OpenAPI Generator)
print_section "Installing OpenJDK"
sudo apt install -y default-jdk
print_message "Java version: $(java -version 2>&1 | head -1)"

# Install Docker
print_section "Installing Docker"
if command -v docker &> /dev/null; then
  print_message "Docker is already installed"
  docker --version
else
  print_message "Removing old Docker packages if any..."
  for pkg in docker.io docker-doc docker-compose docker-compose-v2 podman-docker containerd runc; do
    sudo apt-get remove -y "$pkg" 2>/dev/null || true
  done

  print_message "Adding Docker's official GPG key and repository..."
  sudo apt-get update
  sudo apt-get install -y ca-certificates curl
  sudo install -m 0755 -d /etc/apt/keyrings
  sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  sudo chmod a+r /etc/apt/keyrings/docker.asc

  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
  sudo apt-get update

  print_message "Installing Docker CE and related packages..."
  sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

  # Add current user to docker group (optional, for running docker without sudo)
  if [ "$EUID" -ne 0 ]; then
    sudo usermod -aG docker "$USER"
    print_warning "Added $USER to docker group. You may need to log out and back in for this to take effect."
  fi
fi

# Install Python3 and pip (for install-images.py)
print_section "Installing Python3 and pip"
sudo apt install -y python3 python3-pip python3-venv
pip3 install requests --break-system-packages 2>/dev/null || pip3 install requests

# Verify installations
print_section "Verifying Installations"
echo ""
print_message "Installed versions:"
echo "  make:      $(make --version | head -1)"
echo "  node:      $(node --version)"
echo "  npm:       $(npm --version)"
echo "  go:        $(go version)"
echo "  java:      $(java -version 2>&1 | head -1)"
echo "  docker:    $(docker --version)"
echo "  python3:   $(python3 --version)"

print_section "Setup Complete!"
print_message ""
print_message "Next steps to build CBox:"
print_message ""
print_message "  1. Clone the repository (if not already done):"
print_message "     git clone https://github.com/abilashraghuram/cbox.git"
print_message "     cd cbox"
print_message ""
print_message "  2. Download required binary images:"
print_message "     ./setup/install-images.py"
print_message ""
print_message "  3. Build CBox:"
print_message "     make all"
print_message ""
print_message "  4. Run the server:"
print_message "     sudo ./out/cbox-restserver"
print_message ""
print_warning "Note: You may need to restart your shell or run 'source ~/.bashrc' for PATH changes to take effect."
