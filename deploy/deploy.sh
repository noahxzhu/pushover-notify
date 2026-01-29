#!/bin/bash
set -e

# Ensure we are in the project root
cd "$(dirname "$0")/.."

# Image Name
IMAGE_NAME="pushover-notify:latest"

echo ">>> 1. Building container image..."
podman build -t $IMAGE_NAME -f Containerfile .

echo ">>> 2. Installing Quadlet service file..."
SYSTEMD_DIR="$HOME/.config/containers/systemd"
mkdir -p "$SYSTEMD_DIR"
# Copy from deploy directory
cp deploy/pushover-notify.container "$SYSTEMD_DIR/"

echo ">>> 3. Reloading systemd..."
systemctl --user daemon-reload

echo ">>> 4. Restarting service..."
systemctl --user restart pushover-notify

echo ">>> 5. Enabling User Linger..."
loginctl enable-linger $USER || echo "Warning: Could not enable linger."

echo ""
echo ">>> Deployment Complete! Checking status..."
systemctl --user status pushover-notify --no-pager
