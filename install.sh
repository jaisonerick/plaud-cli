#!/bin/bash
set -euo pipefail

REPO="jaisonerick/plaud-cli"
INSTALL_DIR="/usr/local/bin"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

case "$OS" in
  darwin|linux) ;;
  *)
    echo "Unsupported OS: $OS"
    exit 1
    ;;
esac

echo "Detecting system: ${OS}/${ARCH}"

TAG=$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)

if [ -z "$TAG" ]; then
  echo "Failed to fetch latest release tag"
  exit 1
fi

echo "Latest release: ${TAG}"

URL="https://github.com/${REPO}/releases/download/${TAG}/plaud-cli_${OS}_${ARCH}"

echo "Downloading ${URL}..."
sudo mkdir -p "${INSTALL_DIR}"
curl -sSfL "$URL" | sudo tee "${INSTALL_DIR}/plaud" > /dev/null
sudo chmod 755 "${INSTALL_DIR}/plaud"

echo "Done! Run 'plaud --help' to get started."
