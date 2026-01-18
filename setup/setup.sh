#!/usr/bin/env bash
set -euo pipefail

# Define colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Define variables
CBOX_DIR="./cbox-prebuilt"
LATEST_RELEASE_URL="https://github.com/abilashraghuram/cbox/releases/latest"
RESOURCES_DIR="$CBOX_DIR/resources"
RESOURCES_BIN_DIR="$RESOURCES_DIR/bin"
OUT_DIR="$CBOX_DIR/out"
CONFIG_FILE="$CBOX_DIR/config.yaml"
INSTALL_IMAGES_SCRIPT="$CBOX_DIR/install-images.py"

# Print colored message
print_message() {
  echo -e "${GREEN}[CBox Setup]${NC} $1"
}

print_warning() {
  echo -e "${YELLOW}[Warning]${NC} $1"
}

# Function to download a file
download_file() {
  local url="$1"
  local destination="$2"
  local description="$3"

  print_message "Downloading $description from $url..."
  curl -L -o "$destination" "$url"

  if [ $? -eq 0 ]; then
    print_message "$description downloaded successfully to $destination"
  else
    print_warning "Failed to download $description. Please check the URL and try again."
    exit 1
  fi
}

# Function to make a file executable
make_executable() {
  local file="$1"
  chmod +x "$file"
  print_message "Made $file executable"
}

# Create cbox-prebuilt directory
print_message "Creating CBox directory structure..."
mkdir -p "$CBOX_DIR"
mkdir -p "$RESOURCES_DIR"
mkdir -p "$RESOURCES_BIN_DIR"
mkdir -p "$OUT_DIR"

# Get the latest release URL
# Check if a release version is provided as an argument
if [ -n "${1:-}" ]; then
  RELEASE_TAG="$1"
  print_message "Using specified release version: $RELEASE_TAG"
else
  print_message "No release version specified. Determining latest release..."
  RELEASE_TAG_LATEST=$(curl -s -L "$LATEST_RELEASE_URL" | grep -o "tag/release-[0-9]*" | head -1 | cut -d'/' -f2)

  if [ -z "$RELEASE_TAG_LATEST" ]; then
    print_warning "Could not determine the latest release tag. Please check your network connection or GitHub status, or specify a release version manually (e.g., ./setup.sh release-1)."
    print_warning "Exiting due to inability to determine release tag."
    exit 1
  fi
  RELEASE_TAG="$RELEASE_TAG_LATEST"
  print_message "Using latest release version: $RELEASE_TAG"
fi
RELEASE_URL="https://github.com/abilashraghuram/cbox/releases/download/$RELEASE_TAG"

# Download cbox-restserver
download_file "$RELEASE_URL/cbox-restserver" "$CBOX_DIR/cbox-restserver" "CBox REST Server"
make_executable "$CBOX_DIR/cbox-restserver"

# Download and extract cbox-guestrootfs-ext4.img.tar.gz
print_message "Downloading and extracting CBox Guest Rootfs..."
download_file "$RELEASE_URL/cbox-guestrootfs-ext4.img.tar.gz" "$OUT_DIR/cbox-guestrootfs-ext4.img.tar.gz" "Compressed CBox Guest Rootfs"

print_message "Extracting rootfs image..."
tar -xzf "$OUT_DIR/cbox-guestrootfs-ext4.img.tar.gz" -C "$OUT_DIR"
print_message "Extracted rootfs image to $OUT_DIR/cbox-guestrootfs-ext4.img"

# Download initramfs.cpio.gz
download_file "$RELEASE_URL/initramfs.cpio.gz" "$OUT_DIR/initramfs.cpio.gz" "Initramfs image"

# Download config.yaml
download_file "$RELEASE_URL/config.yaml" "$CONFIG_FILE" "Configuration file"

# Download VERSION file
download_file "$RELEASE_URL/VERSION" "$CBOX_DIR/VERSION" "Version information file"

# Function to display version information
display_version_info() {
  local version_file="$CBOX_DIR/VERSION"

  if [ -f "$version_file" ]; then
    print_message "Installed CBox Version Information:"
    echo -e "${GREEN}================================${NC}"
    while IFS='=' read -r key value; do
      if [ -n "$key" ] && [ -n "$value" ]; then
        printf "${YELLOW}%-15s${NC}: %s\n" "$key" "$value"
      fi
    done < "$version_file"
    echo -e "${GREEN}================================${NC}"
    echo ""
    print_message "To check if you have the latest version, compare this with:"
    print_message "https://github.com/abilashraghuram/cbox/releases/latest"
  else
    print_warning "VERSION file not found. Version information unavailable."
  fi
}

# Display version information
display_version_info

# Download install-images.py
print_message "Downloading install-images.py script..."
curl -L -o "$INSTALL_IMAGES_SCRIPT" "https://raw.githubusercontent.com/abilashraghuram/cbox/main/setup/install-images.py"
chmod +x "$INSTALL_IMAGES_SCRIPT"

# Run install-images.py to download required images
print_message "Running install-images.py to download required images..."
cd "$CBOX_DIR" && ./$(basename "$INSTALL_IMAGES_SCRIPT")

print_message "Setup completed successfully!"
print_message ""
print_message "You can now run the CBox REST server with:"
print_message "  cd $CBOX_DIR && sudo ./cbox-restserver"
print_message ""
print_message "Then use curl or any HTTP client to interact with the API:"
print_message "  curl http://localhost:7000/v1/health"
print_message "  curl -X POST http://localhost:7000/v1/vms -H 'Content-Type: application/json' -d '{\"vmName\": \"my-sandbox\"}'"
