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
# glibc (bookworm) so the runtime binary matches the BtbN linux64 ffmpeg ABI.
FROM golang:1.24-bookworm AS backend-builder
WORKDIR /app/backend
# Install build dependencies for CGO (SQLite)
RUN apt-get update \
	&& apt-get install -y --no-install-recommends gcc libc6-dev \
	&& rm -rf /var/lib/apt/lists/*

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .
# Copy frontend build to backend/public for embedding
COPY --from=frontend-builder /app/frontend/dist ./public

# Build binary
# CGO_ENABLED=1 is required for go-sqlite3
RUN CGO_ENABLED=1 GOOS=linux go build -o teslaxy .

# Stage 3: Runtime
# debian-slim (glibc): BtbN linux64 ffmpeg is a glibc binary. Alpine + gcompat cannot
# execute it (missing symbols / libs), so the build-time h264_nvenc check exited 1
# even though the tarball and sha256 were correct.
FROM debian:bookworm-slim
WORKDIR /app

# Loud build-time encoder check (must run after ffmpeg install).
COPY scripts/check-ffmpeg-nvenc.sh /usr/local/share/teslaxy/check-ffmpeg-nvenc.sh

# Install Runtime Dependencies
# Distro ffmpeg lacks NVENC; ship a pinned BtbN GPL build. CUDA libs still come
# from the NVIDIA Container Toolkit at runtime.
#
# Source: https://github.com/BtbN/FFmpeg-Builds (GPL linux64 shared-free static tarball).
# Release tag autobuild-2026-10-13-13-05, asset ffmpeg-n8.1.3-18-g46d899d7a0-linux64-gpl-8.1.tar.xz.
# SHA256 taken from that release's checksums.sha256 (and re-verified locally before pin).
# linux64 = amd64 only — refuse other architectures rather than silently installing a bad binary.
ARG TARGETARCH
ARG FFMPEG_BTBN_URL="https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-10-13-13-05/ffmpeg-n8.1.3-18-g46d899d7a0-linux64-gpl-8.1.tar.xz"
ARG FFMPEG_BTBN_SHA256="b3506f47a71a2f8cff8c0775112bf02a8a49035e3994232aa5f218058328e0e7"
RUN arch="${TARGETARCH:-}" \
	&& if [ -z "$arch" ]; then arch="$(uname -m)"; fi \
	&& case "$arch" in amd64|x86_64) ;; *) \
		echo "ERROR: BtbN linux64 ffmpeg is amd64-only (TARGETARCH/uname=$arch)" >&2; exit 1 ;; esac \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates tzdata libva2 curl xz-utils \
	&& curl -fsSL -o /tmp/ffmpeg.tar.xz "${FFMPEG_BTBN_URL}" \
	&& echo "${FFMPEG_BTBN_SHA256}  /tmp/ffmpeg.tar.xz" | sha256sum -c - \
	&& mkdir -p /tmp/ffmpeg \
	&& tar -xJf /tmp/ffmpeg.tar.xz -C /tmp/ffmpeg --strip-components=1 \
	&& install -m755 /tmp/ffmpeg/bin/ffmpeg /usr/local/bin/ffmpeg \
	&& install -m755 /tmp/ffmpeg/bin/ffprobe /usr/local/bin/ffprobe \
	&& rm -rf /tmp/ffmpeg /tmp/ffmpeg.tar.xz \
	&& apt-get purge -y curl xz-utils \
	&& apt-get autoremove -y \
	&& rm -rf /var/lib/apt/lists/* \
	&& chmod +x /usr/local/share/teslaxy/check-ffmpeg-nvenc.sh \
	&& /usr/local/share/teslaxy/check-ffmpeg-nvenc.sh

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
ENV RETENTION_ENABLED=false
ENV RETENTION_DRY_RUN=true
ENV RETENTION_MAX_AGE_DAYS=90
ENV RETENTION_MAX_SIZE_GB=300
ENV RETENTION_INTERVAL_HOURS=24
ENV RETENTION_MIN_FILE_AGE_MINUTES=15

# Expose Port
EXPOSE 80

# Entrypoint
CMD ["/usr/local/bin/teslaxy"]
