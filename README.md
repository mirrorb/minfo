# minfo

Local web UI to generate MediaInfo or BDInfo, plus a 4-shot screenshot download.

## Docker Compose (one-click)
```powershell
docker compose up --build
```
Then open http://localhost:8080

For BDISO mounting, the container needs privileges. Add `privileged: true` (or SYS_ADMIN + loop devices) in `docker-compose.yml`.

If you want to use the "server path" input, mount a host folder:
```yaml
services:
  minfo:
    volumes:
      - /path/to/media:/media:ro
```
Then use `/media/...` in the UI.

The container image downloads and includes:
- MediaInfo CLI
- FFmpeg + FFprobe
- util-linux (mount/umount for BDISO)
- BDInfo (dotnetcorecorner/BDInfo, built from source)

Optional env overrides:
- BDINFO_ARGS (extra flags passed to BDInfo CLI)
- MEDIA_ROOT (root path used by server-path autocomplete, default: `/media`)
- WEB_PASSWORD (enable Basic Auth for the web UI)
- MOUNT_BIN (override mount path)
- UMOUNT_BIN (override umount path)

## Web UI (Vite + Vue 3)
Web UI source lives in `webui/`. The Docker build runs the Vite build and embeds the output into the Go binary. The embed directory is `webui/dist` (build output).

Local build:
```bash
cd webui
npm install
npm run build
```

## Requirements (local run)
- MediaInfo CLI
- BDInfo CLI
- FFmpeg and FFprobe

If the binaries are not on PATH, set these environment variables:
- MEDIAINFO_BIN
- BDINFO_BIN
- FFMPEG_BIN
- FFPROBE_BIN
- MOUNT_BIN (required for BDISO screenshots)
- UMOUNT_BIN (required for BDISO screenshots)

## Run
```powershell
# build web UI first
cd webui
npm install
npm run build
cd ..

go run .
```
Then open http://localhost:8080

## Build (Linux x64 / arm64)
```bash
# Linux x64
GOOS=linux GOARCH=amd64 go build -o bin/minfo

# Linux arm64
GOOS=linux GOARCH=arm64 go build -o bin/minfo-arm64
```

## Notes
- Docker build will compile BDInfo from source; it can take a long time on small machines.
- Uploads are saved to a temporary file and removed after each request.
- For very large files or disc folders, use the server path input.
- BD screenshots: accepts BDMV folders or BDISO files. For a folder containing ISO files, the first ISO found is used.
- BDInfo/screenshots on BDISO use loop mount (read-only on source).
