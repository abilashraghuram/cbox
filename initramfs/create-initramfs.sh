#!/bin/bash
INITRAMFS_SRC_DIR="initramfs"
INITRAMFS_WORK_DIR="/tmp/initramfs"
OUT_FILE="out/initramfs.cpio.gz"

# Create directories
for dir in bin dev etc home mnt proc sys usr; do
    mkdir -p ${INITRAMFS_WORK_DIR}/$dir
done

cp resources/bin/busybox ${INITRAMFS_WORK_DIR}/bin/busybox
cp ${INITRAMFS_SRC_DIR}/init.sh ${INITRAMFS_WORK_DIR}/init
chmod +x ${INITRAMFS_WORK_DIR}/init

# Create initramfs image
pushd ${INITRAMFS_WORK_DIR} > /dev/null
find . -print0 | cpio --null -o --format=newc > /tmp/initramfs.cpio
gzip -f /tmp/initramfs.cpio > /tmp/initramfs.cpio.gz
rm -rf /tmp/initramfs.cpio
popd > /dev/null

mv /tmp/initramfs.cpio.gz ${OUT_FILE}
rm -rf /tmp/initramfs.cpio.gz
rm -rf ${INITRAMFS_WORK_DIR}
