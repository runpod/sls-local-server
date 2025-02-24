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

# Determine the package manager and package based on the distribution.
if [ "$ID" = "debian" ] || [ "$ID" = "ubuntu" ]; then
    echo "Using apt-get for Debian/Ubuntu..."
    sudo apt-get update
    PKG="curl"
    INSTALL_CMD="sudo apt-get install -y"
elif [ "$ID" = "fedora" ] || [ "$ID" = "centos" ] || [ "$ID" = "rhel" ]; then
    echo "Using dnf/yum for Fedora/CentOS/RHEL..."
    if command -v dnf >/dev/null 2>&1; then
        INSTALL_CMD="sudo dnf install -y"
    else
        INSTALL_CMD="sudo yum install -y"
    fi
    PKG="wget"
elif [ "$ID" = "arch" ]; then
    echo "Using pacman for Arch Linux..."
    PKG="curl"
    INSTALL_CMD="sudo pacman -S --noconfirm"
elif [ "$ID" = "opensuse" ]; then
    echo "Using zypper for openSUSE..."
    PKG="wget"
    INSTALL_CMD="sudo zypper install -y"
else
    echo "Unsupported or unrecognized distribution: $ID"
    exit 1
fi

echo "Installing $PKG..."
$INSTALL_CMD $PKG
