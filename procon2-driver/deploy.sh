#!/bin/bash

# --- Configuration ---
EXECUTABLE_NAME="procon2-driver"
SERVICE_FILE="procon2-driver.service"
SERVICE_FILE_PATH="/etc/systemd/system/${SERVICE_FILE}"
INSTALL_PATH="/usr/bin/${EXECUTABLE_NAME}"
PROJECT_ROOT=$(pwd)

# --- 1. Compile the Project ---
echo "⚙️  1. Compiling Go project..."
go build -o "${EXECUTABLE_NAME}" ./src
if [ $? -ne 0 ]; then
    echo "❌ Compilation failed. Aborting."
    exit 1
fi
echo "✅ Compilation successful. Binary: ${EXECUTABLE_NAME}"

# --- 2. Move the Executable ---
echo "📦 2. Moving executable to ${INSTALL_PATH}..."
sudo mv "${EXECUTABLE_NAME}" "${INSTALL_PATH}"
if [ $? -ne 0 ]; then
    echo "❌ Failed to move executable. Aborting."
    exit 1
fi
echo "✅ Executable moved and ready."

# --- 3. Move the systemd Service File ---
echo "📝 3. Moving systemd service file to ${SERVICE_FILE_PATH}..."
sudo mv "${SERVICE_FILE}" "${SERVICE_FILE_PATH}"
if [ $? -ne 0 ]; then
    echo "❌ Failed to move service file. Aborting."
    exit 1
fi
echo "✅ Service file moved and ready."

# --- 4. Enable and Start the Service ---
echo "🚀 4. Reloading systemd, enabling, and starting service..."

# Reload the systemd daemon to pick up the new service file
sudo systemctl daemon-reload

# Enable the service to start on boot
sudo systemctl enable "${SERVICE_FILE}"

# Start the service immediately
sudo systemctl start "${SERVICE_FILE}"

# Check the status
echo "--------------------------------------------------------"
sudo systemctl status "${SERVICE_FILE}" --no-pager
echo "--------------------------------------------------------"
echo "🎉 Deployment complete. Check live logs with: journalctl -u ${SERVICE_FILE} -f"
