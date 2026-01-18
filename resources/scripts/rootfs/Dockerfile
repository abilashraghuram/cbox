FROM alpine:3.18

ARG USERNAME=cbox
ARG PASSWORD=cbox0000

# Create user with home directory and add to wheel group for sudo
RUN adduser -D -s /bin/bash ${USERNAME} && \
    echo "${USERNAME}:${PASSWORD}" | chpasswd && \
    addgroup ${USERNAME} wheel

# Set timezone
ENV TZ=Etc/UTC
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

# Update and install common utilities
# Note: Package name mappings from Ubuntu to Alpine
RUN apk update && \
    apk add --no-cache \
    openrc \
    busybox-openrc \
    nmap-ncat \
    bash \
    curl \
    wget \
    vim \
    nano \
    git \
    htop \
    net-tools \
    iputils \
    iproute2 \
    mtr \
    bind-tools \
    tcpdump \
    netcat-openbsd \
    openssh \
    sudo \
    man-db \
    less \
    procps \
    psmisc \
    lsof \
    rsync \
    tar \
    gzip \
    zip \
    unzip \
    ca-certificates \
    tzdata \
    tini \
    python3 \
    py3-pip \
    py3-virtualenv \
    socat \
    strace \
    shadow \
    musl-utils

# Enable wheel group for sudo without password (optional: remove NOPASSWD for security)
RUN echo '%wheel ALL=(ALL) ALL' >> /etc/sudoers

# To support sudo within the guest
RUN chown root:root /usr/bin/sudo && chmod 4755 /usr/bin/sudo

# Create symlinks for commands that are in different locations on Alpine vs Ubuntu
# The guestinit binary expects these at /usr/bin but Alpine puts them in /sbin
RUN ln -sf /sbin/ip /usr/bin/ip && \
    ln -sf /sbin/route /usr/bin/route && \
    ln -sf /bin/hostname /usr/bin/hostname

# Create required directories
RUN mkdir -p /mnt/stateful && chmod 0777 /mnt/stateful

# Set up directory for the vsock server
RUN mkdir -p /tmp/vsockserver && chmod 0755 /tmp/vsockserver

# Set up directory for cmdserver working directory
RUN mkdir -p /tmp/server_files && chmod 0755 /tmp/server_files

# Configure OpenRC for container/VM environment
RUN sed -i 's/^#rc_sys=""/rc_sys="lxc"/' /etc/rc.conf && \
    sed -i 's/^#rc_env_allow=".*"/rc_env_allow="*"/' /etc/rc.conf && \
    sed -i 's/^#rc_provide=".*"/rc_provide="loopback net"/' /etc/rc.conf

# Disable TTY gettys - critical for headless VM boot
# Replace inittab with a minimal version that doesn't spawn TTYs
RUN echo '# Minimal inittab for headless VM' > /etc/inittab && \
    echo '::sysinit:/sbin/openrc sysinit' >> /etc/inittab && \
    echo '::sysinit:/sbin/openrc boot' >> /etc/inittab && \
    echo '::wait:/sbin/openrc default' >> /etc/inittab && \
    echo '::ctrlaltdel:/sbin/reboot' >> /etc/inittab && \
    echo '::shutdown:/sbin/openrc shutdown' >> /etc/inittab && \
    echo 'ttyS0::respawn:/sbin/getty -L ttyS0 115200 vt100' >> /etc/inittab

# Disable TTY and other unnecessary services
RUN rm -f /etc/init.d/hwdrivers \
    /etc/init.d/machine-id \
    /etc/init.d/modules \
    /etc/init.d/modloop

# Create necessary directories for OpenRC
RUN mkdir -p /run/openrc && \
    touch /run/openrc/softlevel

# Ensure /dev/ttyS0 exists for serial console
RUN mkdir -p /dev && \
    mknod -m 666 /dev/ttyS0 c 4 64 2>/dev/null || true

# Add user binaries from the host into the guest rootfs
##############
ARG OUT_DIR=out
ARG RESOURCES_DIR=resources

# Install guestinit service
ARG GUESTINIT_BIN=cbox-guestinit
COPY ${OUT_DIR}/${GUESTINIT_BIN} /usr/local/bin/${GUESTINIT_BIN}
RUN chmod +x /usr/local/bin/${GUESTINIT_BIN}
COPY ${RESOURCES_DIR}/${GUESTINIT_BIN}.openrc /etc/init.d/${GUESTINIT_BIN}
RUN chmod +x /etc/init.d/${GUESTINIT_BIN} && \
    rc-update add ${GUESTINIT_BIN} default

# Install cmdserver service
ARG CMDSERVER_BIN=cbox-cmdserver
COPY ${OUT_DIR}/${CMDSERVER_BIN} /usr/local/bin/${CMDSERVER_BIN}
RUN chmod +x /usr/local/bin/${CMDSERVER_BIN}
COPY ${RESOURCES_DIR}/${CMDSERVER_BIN}.openrc /etc/init.d/${CMDSERVER_BIN}
RUN chmod +x /etc/init.d/${CMDSERVER_BIN} && \
    rc-update add ${CMDSERVER_BIN} default

# Install vsockserver service
ARG VSOCKSERVER_BIN=cbox-vsockserver
COPY ${OUT_DIR}/${VSOCKSERVER_BIN} /usr/local/bin/${VSOCKSERVER_BIN}
RUN chmod +x /usr/local/bin/${VSOCKSERVER_BIN}
COPY ${RESOURCES_DIR}/${VSOCKSERVER_BIN}.openrc /etc/init.d/${VSOCKSERVER_BIN}
RUN chmod +x /etc/init.d/${VSOCKSERVER_BIN} && \
    rc-update add ${VSOCKSERVER_BIN} default

# Install cbox callback library for guest code to call back to the host
# Create the Python site-packages directory for Alpine
RUN mkdir -p /usr/lib/python3.11/site-packages
COPY ${RESOURCES_DIR}/scripts/guest/cbox_callback.py /usr/lib/python3.11/site-packages/cbox_callback.py
COPY ${RESOURCES_DIR}/scripts/guest/cbox_callback.sh /usr/local/bin/cbox_callback
RUN chmod +x /usr/local/bin/cbox_callback

# Alpine uses predictable network interface names by default (eth0)
# No need to disable renaming service like in Ubuntu/systemd
##############
