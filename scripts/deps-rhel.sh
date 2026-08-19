#!/bin/bash
set -e

# Read OS release info
if [ -f /etc/os-release ]; then
    . /etc/os-release
fi

VERSION_MAJOR=$(echo "$VERSION_ID" | cut -d. -f1)

# Apply China mainland mirrors to Rocky Linux if USE_CHINA_MIRROR is enabled
if [ "${USE_CHINA_MIRROR}" = "true" ]; then
    echo "Configuring Rocky Linux repositories to use Aliyun mirrors..."
    if ls /etc/yum.repos.d/rocky*.repo 1>/dev/null 2>&1; then
        sed -e 's|^mirrorlist=|#mirrorlist=|g' \
            -e 's|^#baseurl=http://dl.rockylinux.org/$contentdir|baseurl=https://mirrors.aliyun.com/rockylinux|g' \
            -e 's|^#baseurl=https://dl.rockylinux.org/$contentdir|baseurl=https://mirrors.aliyun.com/rockylinux|g' \
            -i /etc/yum.repos.d/rocky*.repo
    fi
fi

echo "Installing build dependencies for $NAME version $VERSION_ID (Major: $VERSION_MAJOR)..."

# Set up repositories based on version
if [ "$VERSION_MAJOR" -eq 8 ]; then
    echo "RockyLinux/RHEL 8 detected. Enabling powertools..."
    dnf groupinstall -y "Development Tools"
    dnf install -y 'dnf-command(config-manager)'
    dnf config-manager --set-enabled powertools
else
    # RockyLinux/RHEL 9, 10 and newer
    echo "RockyLinux/RHEL 9+ detected. Enabling CRB and EPEL..."
    dnf groupinstall -y "Development Tools"
    dnf install -y 'dnf-command(config-manager)'
    dnf config-manager --set-enabled crb
    dnf install -y epel-release
    
    if [ "${USE_CHINA_MIRROR}" = "true" ]; then
        echo "Configuring EPEL repository to use Aliyun mirrors..."
        if ls /etc/yum.repos.d/epel*.repo 1>/dev/null 2>&1; then
            sed -e 's|^metalink=|#metalink=|g' \
                -e 's|^#baseurl=https://download.fedoraproject.org/pub/epel|baseurl=https://mirrors.aliyun.com/epel|g' \
                -e 's|^#baseurl=http://download.fedoraproject.org/pub/epel|baseurl=https://mirrors.aliyun.com/epel|g' \
                -e 's|^#baseurl=https://download.example/pub/epel|baseurl=https://mirrors.aliyun.com/epel|g' \
                -i /etc/yum.repos.d/epel*.repo
        fi
    fi
fi

# Install packages
dnf install -y \
    clang \
    gcc \
    git \
    krb5-devel \
    libselinux-devel \
    libzstd-devel \
    lz4-devel \
    make \
    libcurl-devel \
    json-c-devel \
    geos-devel \
    proj-devel \
    gdal-devel \
    protobuf-c-devel \
    openssl-devel \
    pam-devel \
    readline-devel \
    rpmdevtools \
    which \
    perl-IPC-Run \
    libxslt-devel \
    zlib-devel \
    wget \
    bzip2-devel \
    libxml2-devel \
    libyaml-devel \
    ninja-build \
    perl \
    perl-devel \
    perl-CPAN \
    systemd-devel \
    tcl \
    tcl-devel \
    python3-devel \
    bison \
    libicu-devel

# Ensure python3-pip is installed
if ! command -v pip3 &> /dev/null; then
    dnf install -y python3-pip
fi

if [ "${USE_CHINA_MIRROR}" = "true" ]; then
    echo "Configuring pip to use Aliyun mirror..."
    pip3 config set global.index-url https://mirrors.aliyun.com/pypi/simple/
fi

# Install meson via pip3. Check for break-system-packages flag for PEP 668 compatibility.
if pip3 install --help | grep -q "break-system-packages"; then
    echo "Installing meson with --break-system-packages..."
    pip3 install meson --break-system-packages
else
    echo "Installing meson..."
    pip3 install meson
fi

# Clean up DNF cache
dnf clean all

echo "RockyLinux/RHEL dependency installation complete!"
