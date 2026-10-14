# Stage 1: Build Frontend
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci --no-audit --no-fund --prefer-offline 2>&1
COPY frontend/ .
RUN npm run build
# Verify the build output exists (better error if tsc/vite fails)
RUN ls -la dist/ || (echo "ERROR: Frontend build did not produce dist/ directory" && exit 1)

# Stage 2: Build Backend
FROM golang:1.24-alpine AS backend-builder
WORKDIR /app/backend
# Install build dependencies for CGO (SQLite)
RUN apk add --no-cache gcc musl-dev

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .
# Copy frontend build to backend/public for embedding
COPY --from=frontend-builder /app/frontend/dist ./public

# Build binary
# CGO_ENABLED=1 is required for go-sqlite3
RUN CGO_ENABLED=1 GOOS=linux go build -o teslaxy .

# Stage 3: Runtime
FROM alpine:3.21
WORKDIR /app

# Install Runtime Dependencies
# Alpine's apk ffmpeg is built without NVENC. Under `runtime: nvidia` that made every
# export fail with "Unknown encoder 'h264_nvenc'" once nvidia-smi succeeded (H5).
# Ship a pinned BtbN GPL static build that includes h264_nvenc; CUDA libs still come
# from the NVIDIA Container Toolkit at runtime. gcompat covers glibc symbols on musl.
# Pin: BtbN autobuild-2026-10-13-13-05 / ffmpeg-n8.1.3 linux64-gpl-8.1
ARG FFMPEG_BTBN_URL="https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-10-13-13-05/ffmpeg-n8.1.3-18-g46d899d7a0-linux64-gpl-8.1.tar.xz"
RUN apk add --no-cache ca-certificates tzdata libva gcompat curl xz \
	&& curl -fsSL -o /tmp/ffmpeg.tar.xz "${FFMPEG_BTBN_URL}" \
	&& mkdir -p /tmp/ffmpeg \
	&& tar -xJf /tmp/ffmpeg.tar.xz -C /tmp/ffmpeg --strip-components=1 \
	&& install -m755 /tmp/ffmpeg/bin/ffmpeg /usr/local/bin/ffmpeg \
	&& install -m755 /tmp/ffmpeg/bin/ffprobe /usr/local/bin/ffprobe \
	&& rm -rf /tmp/ffmpeg /tmp/ffmpeg.tar.xz \
	&& apk del curl xz \
	&& /usr/local/bin/ffmpeg -hide_banner -encoders 2>/dev/null | grep -q h264_nvenc

# Copy Binary
COPY --from=backend-builder /app/backend/teslaxy /usr/local/bin/teslaxy

# Copy Changelog
COPY CHANGELOG.md /app/CHANGELOG.md

# Create Directories
RUN mkdir -p /footage /config /config/logs /config/exports

# Environment Variables
ENV FOOTAGE_PATH=/footage
ENV CONFIG_PATH=/config
ENV PORT=80
ENV GIN_MODE=release

# Expose Port
EXPOSE 80

# Entrypoint
CMD ["/usr/local/bin/teslaxy"]
