# WiFi Radar (Go)

Minimal MVP that samples Wi-Fi link metrics via `iw`, exposes an HTTP API + SSE, and renders a small radar/gauge UI.

## Run

```bash
go run ./cmd/server --if wlp0s20f3 --interval 500ms --listen 127.0.0.1:8888
```

Then open `http://127.0.0.1:8888/`.

## Endpoints

- `GET /api/status`
- `GET /api/best`
- `GET /api/stream` (SSE)
