#!/bin/bash
set -e

# Function to check if a command exists
check_command() {
    if ! command -v "$1" &> /dev/null; then
        echo "Error: $1 is not installed."
        exit 1
    fi
}

echo "Checking for required tools..."
check_command go
check_command node
check_command npm
check_command gcc

# Check Go version
GO_VERSION_STRING=$(go version | awk '{print $3}' | sed 's/go//')
echo "Found Go version: $GO_VERSION_STRING"

# Extract major and minor version
# Use cut to handle potential trailing characters or extra dots
MAJOR=$(echo "$GO_VERSION_STRING" | cut -d. -f1)
MINOR=$(echo "$GO_VERSION_STRING" | cut -d. -f2)

# Check if MAJOR or MINOR are not numbers (in case of weird version strings)
if ! [[ "$MAJOR" =~ ^[0-9]+$ ]] || ! [[ "$MINOR" =~ ^[0-9]+$ ]]; then
    echo "Warning: Could not parse Go version correctly. Proceeding with caution."
else
    if [ "$MAJOR" -lt 1 ] || ([ "$MAJOR" -eq 1 ] && [ "$MINOR" -lt 24 ]); then
       echo "Error: Go version 1.24 or higher is required. Found $GO_VERSION_STRING"
       exit 1
    fi
fi

echo "Installing backend dependencies..."
(cd backend && go mod download)

echo "Installing frontend dependencies..."
(cd frontend && npm install)

echo "Setup complete!"
