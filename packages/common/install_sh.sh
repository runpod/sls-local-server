#!/bin/sh
# This script detects the Linux distribution and installs either curl or wget
# using the appropriate package manager, in a POSIX-compliant way.

# Ensure /etc/os-release exists for detecting the distro.
if [ ! -f /etc/os-release ]; then
    echo "Cannot detect OS. Exiting."
    exit 1
fi

. /etc/os-release

echo "Detected Linux: $PRETTY_NAME"

export DEBIAN_FRONTEND=noninteractive
export TZ=Etc/UTC

# Determine the package manager and package based on the distribution.
if [ "$ID" = "debian" ] || [ "$ID" = "ubuntu" ]; then
    echo "Using apt-get for Debian/Ubuntu..."
    apt-get update
    apt-get install build-essential tcl pkg-config libssl-dev curl wget -y && \
    curl -O https://download.redis.io/redis-stable.tar.gz && \
    tar xzf redis-stable.tar.gz && \
    cd redis-stable && \
    make BUILD_TLS=yes && \
    make install
elif [ "$ID" = "fedora" ] || [ "$ID" = "centos" ] || [ "$ID" = "rhel" ]; then
    echo "Using dnf/yum for Fedora/CentOS/RHEL..."
    if command -v dnf >/dev/null 2>&1; then
        INSTALL_CMD="dnf install -y"
    else
        INSTALL_CMD="yum install -y"
    fi
    PKG="wget redis-server"
elif [ "$ID" = "arch" ]; then
    echo "Using pacman for Arch Linux..."
    PKG="curl redis-server"
    INSTALL_CMD="pacman -S --noconfirm"
elif [ "$ID" = "opensuse" ]; then
    echo "Using zypper for openSUSE..."
    PKG="wget redis-server"
    INSTALL_CMD="zypper install -y"
else
    echo "Unsupported or unrecognized distribution: $ID"
    exit 1
fi

echo "Installing $PKG..."
$INSTALL_CMD $PKG