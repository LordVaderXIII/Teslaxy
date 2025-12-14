#!/bin/bash
# Unraid Deployment Script for Teslaxy
# This script builds and deploys the Teslaxy container on Unraid.
# It includes logic to report build/run failures to Jules (if API key is provided).

# Set variables
REPO_PATH="/mnt/user/appdata/custom-docker-builds/Teslaxy"
IMAGE_TAG="teslaxy:latest"
CONTAINER_NAME="Teslaxy"
CLIPS_PATH="/mnt/user/teslacam"
PORT_MAPPING="8089:80/tcp"
# CPU Pinning: Pin to specific cores (e.g., 4,5). Comment out to disable.
CPU_PINNING="--cpuset-cpus=4,5"
JULES_SOURCE="sources/github/LordVaderXIII/Teslaxy"
# Jules API Key: Set via environment variable JULES_API_KEY or edit below.
JULES_API_KEY="${JULES_API_KEY:-redacted}"
SENT_LOG_FILE="/tmp/jules_sent.log"  # Persistent log for deduplication

# Function to send logs to Jules for bug fix request
send_logs_to_jules() {
  local error_logs="$1"

  if [ "$JULES_API_KEY" == "redacted" ] || [ -z "$JULES_API_KEY" ]; then
    echo "Jules API Key not set. Skipping bug report."
    return
  fi

  # Sanitize sensitive data (e.g., API key)
  local sanitized_logs=$(echo "$error_logs" | sed "s/$JULES_API_KEY/[REDACTED]/g")
  # Compute hash of sanitized logs for deduplication
  local logs_hash=$(echo -n "$sanitized_logs" | md5sum | awk '{print $1}')
  # Check if hash already sent
  if grep -q "^$logs_hash$" "$SENT_LOG_FILE" 2>/dev/null; then
    echo "Skipping duplicate Jules request for this error."
    return
  fi
  # Use jq to safely construct JSON payload
  local payload=$(jq -n \
    --arg prompt "Bug fix request for Teslaxy Docker build/run on Unraid: $sanitized_logs" \
    --arg title "Teslaxy Unraid Docker Bug Fix" \
    --arg source "$JULES_SOURCE" \
    --arg branch "main" \
    '{
      "prompt": $prompt,
      "title": $title,
      "sourceContext": {
        "source": $source,
        "githubRepoContext": {
          "startingBranch": $branch
        }
      },
      "requirePlanApproval": false,
      "automationMode": "AUTO_CREATE_PR"
    }')
  echo "Sending logs to Jules..."  # Log indication
  local response=$(curl -X POST https://jules.googleapis.com/v1alpha/sessions \
    -H "Content-Type: application/json" \
    -H "X-Goog-Api-Key: $JULES_API_KEY" \
    -d "$payload" || echo "Warning: Failed to send logs to Jules")
  if [[ "$response" != *"Warning: Failed to send logs to Jules"* ]]; then
    echo "$logs_hash" >> "$SENT_LOG_FILE"  # Log hash if sent successfully
  fi
}

# Step 1: Create repo dir and clone if not exists
if [ ! -d "$REPO_PATH" ]; then
  mkdir -p "$REPO_PATH"
fi
cd "$REPO_PATH" || { echo "Error: Repo path not found"; exit 1; }

if [ ! -d .git ]; then
  echo "Cloning repository..."
  git clone https://github.com/LordVaderXIII/Teslaxy.git . || { echo "Error: Git clone failed"; exit 1; }
fi

# Step 2: Fetch and switch to main branch
echo "Fetching latest changes..."
git fetch origin || { echo "Error: Git fetch failed"; exit 1; }
git checkout main --force || { echo "Error: Failed to switch to main"; exit 1; }

# Step 3: Pull latest from main
git pull origin main || { echo "Error: Git pull failed"; exit 1; }

# Debug: Show status and current branch
git status
git branch --show-current

# Step 4: Prune unused Docker data and build cache
echo "Pruning Docker data..."
docker system prune -a -f --volumes
docker builder prune -a -f

# Step 5: Rebuild the image from repo root using specified Dockerfile
echo "Building Docker image..."
LOG_FILE="/tmp/teslaxy_build.log"
docker build -f docker/Dockerfile -t "$IMAGE_TAG" . > "$LOG_FILE" 2>&1 || { send_logs_to_jules "$(cat $LOG_FILE)"; echo "Error: Docker build failed. See $LOG_FILE"; exit 1; }

# Step 6: Stop and remove old container if exists
if [ "$(docker ps -q -f name=$CONTAINER_NAME)" ]; then
  echo "Stopping existing container..."
  docker stop "$CONTAINER_NAME" || { echo "Error: Stop failed"; exit 1; }
fi
docker rm "$CONTAINER_NAME" || true # Ignore if not exists

# Step 7: Deploy new container with CPU pinning, GPU, and env vars
echo "Starting new container..."
docker run -d \
  --name "$CONTAINER_NAME" \
  --restart unless-stopped \
  -p "$PORT_MAPPING" \
  -v "$CLIPS_PATH":/footage:ro \
  -v "$REPO_PATH/config":/config \
  -e AUTH_ENABLED=false \
  -e GIN_MODE=release \
  -e JULES_SOURCE="$JULES_SOURCE" \
  $CPU_PINNING \
  --runtime=nvidia \
  "$IMAGE_TAG" > "$LOG_FILE" 2>&1 || { send_logs_to_jules "$(cat $LOG_FILE)"; echo "Error: Docker run failed. See $LOG_FILE"; exit 1; }

# Step 8: Prune orphaned/dangling images
docker image prune -f --filter "dangling=true" || { echo "Warning: Prune failed, but update complete"; }

echo "Update complete. Check logs: docker logs $CONTAINER_NAME"
