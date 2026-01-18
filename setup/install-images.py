#!/usr/bin/env python3

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

    # Download files
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

    for file_info in files_to_download:
        try:
            download_file(
                file_info["url"],
                file_info["destination"],
                file_info["executable"]
            )
            print(f"Successfully downloaded {file_info['destination']}")
        except Exception as e:
            print(f"Error downloading {file_info['url']}: {str(e)}")
            exit(1)

    print("All files downloaded successfully!")

if __name__ == "__main__":
    main()
