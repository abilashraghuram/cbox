#!/usr/bin/env python3
"""
CBox Install Images Script

Downloads the required binary images for running CBox MicroVMs:
- vmlinux.bin: Linux kernel image
- cloud-hypervisor: Cloud Hypervisor VMM binary
- busybox: BusyBox for initramfs
"""

import os
import requests
import stat
from pathlib import Path

def ensure_directory_exists(path):
    """Create directory if it doesn't exist."""
    Path(path).mkdir(parents=True, exist_ok=True)

def download_file(url, destination, make_executable=False):
    """Download a file from URL to destination."""
    print(f"Downloading {url} to {destination}...")

    # For GitHub blob URLs, we need to get the raw content URL
    if "blob" in url:
        url = url.replace("github.com", "raw.githubusercontent.com").replace("/blob/", "/")

    response = requests.get(url, stream=True)
    response.raise_for_status()

    with open(destination, 'wb') as f:
        for chunk in response.iter_content(chunk_size=8192):
            f.write(chunk)

    if make_executable:
        # Make the file executable (chmod +x)
        current_permissions = os.stat(destination).st_mode
        os.chmod(destination, current_permissions | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

def main():
    # Ensure resources/bin directory exists
    bin_dir = "resources/bin"
    ensure_directory_exists(bin_dir)

    # Files to download
    # Using the same sources as arrakis since these are standard components
    files_to_download = [
        {
            "url": "https://github.com/abshkbh/arrakis-images/blob/main/guest/kernel/vmlinux.bin",
            "destination": f"{bin_dir}/vmlinux.bin",
            "executable": False
        },
        {
            "url": "https://github.com/cloud-hypervisor/cloud-hypervisor/releases/download/v44.0/cloud-hypervisor-static",
            "destination": f"{bin_dir}/cloud-hypervisor",
            "executable": True
        },
        {
            "url": "https://github.com/abshkbh/arrakis-images/blob/main/busybox",
            "destination": f"{bin_dir}/busybox",
            "executable": True
        }
    ]

    print("=" * 60)
    print("CBox Image Installer")
    print("=" * 60)
    print(f"Installing to: {os.path.abspath(bin_dir)}")
    print()

    for file_info in files_to_download:
        try:
            print(f"[*] Downloading {file_info['description']}...")
            download_file(
                file_info["url"],
                file_info["destination"],
                file_info["executable"]
            )
            size_mb = os.path.getsize(file_info["destination"]) / (1024 * 1024)
            print(f"    ✓ Downloaded {file_info['destination']} ({size_mb:.2f} MB)")
        except Exception as e:
            print(f"    ✗ Error downloading {file_info['url']}: {str(e)}")
            exit(1)

    print()
    print("=" * 60)
    print("All files downloaded successfully!")
    print("=" * 60)
    print()
    print("Directory contents:")
    for f in os.listdir(bin_dir):
        filepath = os.path.join(bin_dir, f)
        size_mb = os.path.getsize(filepath) / (1024 * 1024)
        print(f"  - {f} ({size_mb:.2f} MB)")

if __name__ == "__main__":
    main()
