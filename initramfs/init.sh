#!/bin/busybox sh
echo "In initramfs"

LOWER_RO_DEVICE=/dev/vda
WRITABLE_RW_DEVICE=/dev/vdb

LOWER_RO=/mnt/ro
WRITABLE_RW=/mnt/rw
UPPER=${WRITABLE_RW}/upper
WORK=${WRITABLE_RW}/work

NEWROOT=${WRITABLE_RW}/newroot
NEWROOT_LOWER_RO=${NEWROOT}/ro
NEWROOT_WRITABLE_RW=${NEWROOT}/rw


echo "Setup essential mounts"
/bin/busybox mount -t devtmpfs none /dev
/bin/busybox mount -t proc proc /proc
/bin/busybox mount -t sysfs sysfs /sys
/bin/busybox mount -t tmpfs inittemp /mnt

echo "Setting up overlayroot..."

# 3. Mount read-only rootfs
echo "Mounting read-only rootfs from $LOWER_RO_DEVICE to $LOWER_RO"
/bin/busybox mkdir -p ${LOWER_RO}
/bin/busybox mount -t ext4 ${LOWER_RO_DEVICE} ${LOWER_RO}
if [ $? -ne 0 ]; then
    echo "Error mounting read-only rootfs!"
    exec /bin/busybox sh  # Drop to shell for debugging
    return 1
else
    echo "Read-only rootfs mounted successfully."
fi

# 2. Mount writable device
echo "Mounting writable device $WRITABLE_RW_DEVICE to $WRITABLE_RW"
/bin/busybox mkdir -p ${WRITABLE_RW}
# TODO: Add ro for good measure.
/bin/busybox mount -t ext4 ${WRITABLE_RW_DEVICE} ${WRITABLE_RW}
if [ $? -ne 0 ]; then
    echo "Error mounting writable device!"
    exec /bin/busybox sh  # Drop to shell for debugging
    return 1
else
    echo "Writable device mounted successfully."
fi

# 3. Create upper and work directories
echo "Creating upper and work directories in $WRITABLE_RW"
/bin/busybox mkdir -p ${UPPER}
/bin/busybox mkdir -p ${WORK}
/bin/busybox mkdir -p ${NEWROOT}
/bin/busybox mkdir -p ${NEWROOT_LOWER_RO}
/bin/busybox mkdir -p ${NEWROOT_WRITABLE_RW}

# 4. Mount overlayfs
echo "Mounting overlayfs to $NEWROOT"
/bin/busybox mount -t overlay overlay -o lowerdir=${LOWER_RO},upperdir=${UPPER},workdir=${WORK} ${NEWROOT}
if [ $? -ne 0 ]; then
    echo "Error mounting overlayfs!"
    exec /bin/busybox sh  # Drop to shell for debugging
    return 1
else
    echo "Overlayfs mounted successfully."
fi

echo "Verifying /mnt/overlay is a mount point..."
echo $(/bin/busybox mount)

echo "Switching root to ${NEWROOT}"
exec switch_root ${NEWROOT} /sbin/init
