# Teslaxy

Teslaxy is a modern, self-hosted web application designed for viewing Tesla Dashcam and Sentry Mode clips. It provides a unified interface to browse, playback, and export your Tesla footage, complete with telemetry data overlays.

## Features

- **Multi-Camera Playback:** Synchronized viewing of Front, Back, Left, and Right camera angles.
- **Sentry & Dashcam Support:** Automatically organizes clips from `SentryClips`, `SavedClips`, and `RecentClips`.
- **Telemetry Overlay:** Visualization of vehicle data including speed, gear, steering angle, and autopilot state (extracted from video SEI data).
- **Map View:** GPS location tracking synchronized with video playback.
- **Hardware Acceleration:** Supports NVIDIA NVENC for fast video processing and exporting.
- **Modern UI:** Responsive, clean interface built with React and Tailwind CSS.
- **Playback Controls:** Skip and rewind 5 seconds during playback.

## Upcoming Features

- 1 button playback speed .5, 1, 1.5, 2 and 4 times
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
     - /path/to/your/TeslaCam:/footage:ro
     - ./config:/config
   ```

   *Note: The `/footage` volume should point to the directory containing folders like `SentryClips`, `SavedClips`, or `RecentClips`.*

3. **Run the application:**

   ```bash
   docker-compose -f docker/docker-compose.yml up -d --build
   ```

   This command will build the frontend and backend, then start the container.

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
