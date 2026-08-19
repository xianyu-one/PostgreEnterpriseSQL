#!/bin/bash
set -e

# Apply China mainland mirrors if USE_CHINA_MIRROR is enabled
if [ "${USE_CHINA_MIRROR}" = "true" ]; then
    echo "Configuring Debian/Ubuntu to use China mainland mirrors (Aliyun)..."
    if [ -f /etc/apt/sources.list ]; then
        sed -i 's/[a-z\.]*archive.ubuntu.com/mirrors.aliyun.com/g' /etc/apt/sources.list
        sed -i 's/security.ubuntu.com/mirrors.aliyun.com/g' /etc/apt/sources.list
        sed -i 's/deb.debian.org/mirrors.aliyun.com/g' /etc/apt/sources.list
        sed -i 's/security.debian.org/mirrors.aliyun.com/g' /etc/apt/sources.list
        sed -i 's/ports.ubuntu.com/mirrors.aliyun.com\/ubuntu-ports/g' /etc/apt/sources.list
    fi
    if [ -d /etc/apt/sources.list.d ]; then
        find /etc/apt/sources.list.d/ -type f \( -name "*.list" -o -name "*.sources" \) | while read -r file; do
            sed -i 's/[a-z\.]*archive.ubuntu.com/mirrors.aliyun.com/g' "$file"
            sed -i 's/security.ubuntu.com/mirrors.aliyun.com/g' "$file"
            sed -i 's/deb.debian.org/mirrors.aliyun.com/g' "$file"
            sed -i 's/security.debian.org/mirrors.aliyun.com/g' "$file"
            sed -i 's/ports.ubuntu.com/mirrors.aliyun.com\/ubuntu-ports/g' "$file"
        done
    fi
fi

# Set timezone and noninteractive frontend for apt
export DEBIAN_FRONTEND=noninteractive
export TZ=Asia/Taipei

echo "Updating system packages..."
apt-get update && apt-get upgrade -y

# Read OS release info
if [ -f /etc/os-release ]; then
    . /etc/os-release
fi

echo "Installing build dependencies for $NAME $VERSION_ID..."

# Common packages for all Debian/Ubuntu versions
PACKAGES=(
    build-essential
    libreadline-dev
    zlib1g-dev
    libsystemd-dev
    tcl-dev
    libperl-dev
    meson
    gcc
    libpq-dev
    libssl-dev
    libxml2-dev
    pkg-config
    liblz4-dev
    libzstd-dev
    libbz2-dev
    libz-dev
    libyaml-dev
    libssh2-1-dev
    python3-dev
    libselinux1-dev
    libpam0g-dev
    libkrb5-dev
    git
    wget
    libcurl4-openssl-dev
    libjson-c-dev
    libgeos-dev
    libproj-dev
    libgdal-dev
    libprotobuf-c-dev
    protobuf-c-compiler
    bison
    flex
    libicu-dev
    libxml2-utils
    docbook-xml 
    docbook-xsl 
    libxml2-utils 
    xsltproc
)

# Only install python3-distutils on older Ubuntu versions (like Ubuntu 22.04)
# Ubuntu 24.04/26.04 and Debian 12 do not have python3-distutils package (it was removed/deprecated in Python 3.12)
if [ "$ID" = "ubuntu" ] && [ "$VERSION_ID" = "22.04" ]; then
    echo "Ubuntu 22.04 detected, adding pkg to package list..."
    PACKAGES+=(python3-distutils)
fi




apt-get install -y "${PACKAGES[@]}"

# Clean up apt caches
apt-get clean
rm -rf /var/lib/apt/lists/*

echo "Debian/Ubuntu dependency installation complete!"
