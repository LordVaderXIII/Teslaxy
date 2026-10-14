# Teslaxy

Teslaxy is a modern, self-hosted web application designed for viewing Tesla Dashcam and Sentry Mode clips. It provides a unified interface to browse, playback, and export your Tesla footage, complete with telemetry data overlays.

THIS APP IS IN HEAVY DEVELOPMENT AND MOST FEATURES ARE IN SOME SORT OF BROKEN STATE.

## Features

- **Multi-Camera Playback:** Synchronized viewing of Front, Back, Left, and Right camera angles.
- **Sentry & Dashcam Support:** Automatically organizes clips from `SentryClips`, `SavedClips`, and `RecentClips`.
- **Telemetry Overlay:** Visualization of vehicle data including speed, gear, steering angle, and autopilot state (extracted from video SEI data).
- **Map View:** GPS location tracking synchronized with video playback.
- **Hardware Acceleration:** Supports NVIDIA NVENC for fast video processing and exporting.
- **Modern UI:** Responsive, clean interface built with React and Tailwind CSS.
- **Playback Controls:** Skip and rewind 5 seconds during playback.
- **Playback Speed:** Cycle through 0.5x, 1x, 1.5x, 2x, and 4x playback speeds.
- **Dynamic Camera Grid:** Click any camera view to swap it with the main view, allowing focus on specific angles.
- **Footage Retention:** Optional scheduled pruning by age and size cap (oldest first). Tesla SavedClips and user-protected clips are never deleted. Dry-run mode logs what would be freed.

## Upcoming Features

- Export functionality stage 1, snip a section of a clip using red and green bracket from one camera and export to mp4
- Export funcxtionality stage 2, pick which camera view is included and they are stiched together in the export
- Basic authentication (optional Docker variable of auth=true/false to reset), off by default, user can set password in app. User account area created to support this
- Logs now visable in User account
- Logs have self healing reporting built in, this flags errors and asks the user to send to Jules via Jules API, will require API key
- 3D view customisations, allow the user to move each camera view around to better stitch them together. This will allow us to get a much better 3D experiance
- Phone integration stage 1, 3D on mobile using the phones gyro for movement in the space for iOS
- Phone integration stage 2, mobile notifications
- AI integration stage 1, build the ability to plug the AI of choice into the app, providing API integration for Gemini, ChatGPT, Grok, Claude
- AI integration stage 2, Allow the user to use the AI in search
- Extract telemetry data from the video feeds (post Christmas 2025 update) and store in DB
- Implement simple dashboards for telemetry
- AI integration stage 3, Analyse Tesla telemetry data once this is availible

## Getting Started with Docker

The easiest way to run Teslaxy is using Docker.

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and [Docker Compose](https://docs.docker.com/compose/install/)
- **Optional:** NVIDIA GPU drivers and [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html) for hardware acceleration.

### Installation

1. **Clone the repository:**
   ```bash
   git clone https://github.com/yourusername/teslaxy.git
   cd teslaxy
   ```

2. **Configure `docker/docker-compose.yml`:**
   Open `docker/docker-compose.yml` and update the volume path for your footage.

   ```yaml
   volumes:
     # Change this path to your actual TeslaCam drive/folder location
     # Use :ro for read-only browsing. Use :rw (or omit the mode) if retention prune is enabled.
     - /path/to/your/TeslaCam:/footage:ro
     - ./config:/config
   ```

   *Note: The `/footage` volume should point to the directory containing folders like `SentryClips`, `SavedClips`, or `RecentClips`.*
   *On this LAN, mount by IP (e.g. `192.168.86.41`), never a `.local` hostname.*

3. **Run the application:**

   ```bash
   docker-compose -f docker/docker-compose.yml up -d --build
   ```

   This command will build the frontend and backend, then start the container.

   **Pinned Docker network:** Compose attaches Teslaxy to a user-defined network `teslaxy-net` with subnet `172.16.242.0/24`. That subnet is pinned to stay clear of common LAN ranges `192.168.3.0/24` and `192.168.86.0/24` (and reserved `.240`/`.241` addresses elsewhere on the host). If you install with unRAID `docker run` instead of Compose, create and attach the same pinned network (for example `docker network create --subnet=172.16.242.0/24 teslaxy-net` and `--network teslaxy-net` on `docker run`).

4. **Access the interface:**
   Open your browser and navigate to `http://localhost:8080`.

### Configuration

You can configure the application via environment variables in `docker-compose.yml`:

| Variable | Description | Default |
|----------|-------------|---------|
| `FOOTAGE_PATH` | Internal path to footage directory | `/footage` |
| `CONFIG_PATH` | Internal path for DB and logs | `/config` |
| `PORT` | Internal port | `80` |
| `GIN_MODE` | Gin framework mode | `release` |
| `RETENTION_ENABLED` | Run scheduled footage pruning | `false` |
| `RETENTION_DRY_RUN` | Log candidates only; delete nothing | `true` |
| `RETENTION_MAX_AGE_DAYS` | Delete unprotected clips older than N days | `90` |
| `RETENTION_MAX_SIZE_GB` | Keep total indexed footage under this many GB | `300` |
| `RETENTION_INTERVAL_HOURS` | Hours between scheduled passes | `24` |
| `RETENTION_MIN_FILE_AGE_MINUTES` | Skip files with newer mtime (in-progress writes) | `15` |

Settings also persist in `$CONFIG_PATH/retention.json` and via:

- `GET` / `PUT` `/api/retention/settings`
- `POST` `/api/retention/run` with optional `{"dry_run":true}`
- `POST` `/api/clips/:id/protect` with `{"protected":true}`

**Protection:** Tesla `SavedClips` (`event=Saved`) and any clip with `protected=true` are never pruned.

**Safety:** Deletes walk every path component with `openat(O_NOFOLLOW)` / `unlinkat` and refuse symlinked parents. Only selected clip files are removed (not whole event folders). Recently written files are skipped. Soft-delete happens before unlink; pending tombstones are finished on the next run and the scanner will not recreate them. Partial failures keep DB rows for files that remain on disk.

**Auth:** Retention/protect write APIs always require a Bearer token (even if `AUTH_ENABLED=false`), `Content-Type: application/json`, a CSRF header (`X-Requested-With` or `X-Teslaxy-CSRF`), and a same-origin `Origin`/`Referer` when those headers are present.

**Defaults rationale:** 90 days keeps a useful Sentry/Recent history; ~300 GB leaves headroom on a share that is already near capacity (e.g. Valhalla at ~91% full). Start with `RETENTION_DRY_RUN=true` and inspect logs before enabling deletes. Destructive prune requires a writable `/footage` mount.

### GPU Support

To enable NVIDIA hardware acceleration for smoother playback processing and faster exports:

1. Ensure the NVIDIA Container Toolkit is installed on your host.
2. The `docker-compose.yml` file already includes the necessary `deploy` section for GPU resources. Ensure it is uncommented or active (it is active by default in the provided file).

## Development

To develop locally without Docker:

### Backend
1. Install Go 1.23+ and GCC (for SQLite).
2. Navigate to `backend/`.
3. Run `go mod download`.
4. Set environment variables (e.g., `export FOOTAGE_PATH=/path/to/footage`).
5. Run `go run main.go`.

### Frontend
1. Install Node.js 18+.
2. Navigate to `frontend/`.
3. Run `npm install`.
4. Run `npm run dev`.
