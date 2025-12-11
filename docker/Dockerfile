# Stage 1: Build Frontend
FROM node:18-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ .
RUN npm run build

# Stage 2: Build Backend
FROM golang:1.23-alpine AS backend-builder
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
FROM alpine:3.19
WORKDIR /app

# Install Runtime Dependencies
# ffmpeg for export
# nvidia-driver libs are mounted by the runtime usually, but we need ffmpeg.
RUN apk add --no-cache ffmpeg tzdata ca-certificates libva

# Copy Binary
COPY --from=backend-builder /app/backend/teslaxy /usr/local/bin/teslaxy

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
