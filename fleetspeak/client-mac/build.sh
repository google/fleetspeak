#!/bin/sh

realpath() {
    [[ $1 = /* ]] && echo "$1" || echo "$PWD/${1#./}"
}

CONFIGS_DIR=$(pwd)
FS_BINARY=$(realpath $1)
FS_VERSION=$(cat ../../VERSION)
COMPONENT_PKG_NAME="fleetspeakd-component.xar"
PRODUCT_ARCHIVE_NAME="fleetspeakd-${FS_VERSION}.pkg"

rm -rf ./work
mkdir work
cd work

mkdir -p pkg_root/Library/LaunchDaemons
mkdir -p pkg_root/etc/fleetspeak/services
mkdir -p pkg_root/etc/fleetspeak/textservices

# We install the Fleetspeak binary in /usr/local/bin, and
# not /usr/sbin (as we do for Linux), because MacOS won't let us:
# https://en.wikipedia.org/wiki/System_Integrity_Protection.
mkdir -p pkg_root/usr/local/bin

# Copy postinstall script.
mkdir install-scripts
cp "${CONFIGS_DIR}/postinstall" install-scripts
chmod +x install-scripts/*

cp "${CONFIGS_DIR}/com.google.code.fleetspeak.plist" pkg_root/Library/LaunchDaemons
cp "${CONFIGS_DIR}/communicator.txt" pkg_root/etc/fleetspeak
cp "${FS_BINARY}" pkg_root/usr/local/bin/fleetspeakd

# Set permissions for files and directories in the payload.
find pkg_root -type d -exec chmod 755 {} \;
find pkg_root -type f -exec chmod 644 {} \;
chmod +x pkg_root/usr/local/bin/fleetspeakd

# Create a component package containing files to be installed.
pkgbuild \
  --root pkg_root \
  --identifier com.google.code.fleetspeak \
  --version "${FS_VERSION}" \
  --scripts install-scripts \
  "${COMPONENT_PKG_NAME}"

# Copy over distribution file.
cp "${CONFIGS_DIR}/Distribution.xml" ./
# Interpolate the version in the distribution file.
# The tilde after the 'i' flag is the suffix to use for the backup file
# created by sed.
sed -i~ "s:\$VERSION:${FS_VERSION}:g" Distribution.xml
rm -f 'Distribution.xml~'

# Create final product archive.
productbuild --distribution Distribution.xml --package-path "${COMPONENT_PKG_NAME}" "${PRODUCT_ARCHIVE_NAME}"

