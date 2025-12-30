# WiFi Radar (Go)

Minimal MVP that scans Wi-Fi signal levels via `iw`, exposes an HTTP API + SSE, and renders a small radar/gauge UI.

## Run (scan mode, default)

```bash

go run ./cmd/server

or

go run ./cmd/server --if wlp0s20f3 --interval 500ms --listen 0.0.0.0:8888
```

On start, it scans available networks and prompts you to pick one. The app then keeps scanning and tracks that network's RSSI without connecting. RX/TX rates are not available in scan mode.

You can skip the prompt:

```bash
go run ./cmd/server --if wlp0s20f3 --ssid "MyWiFi"
```

or:

```bash
go run ./cmd/server --if wlp0s20f3 --bssid aa:bb:cc:dd:ee:ff
```

## Run (link mode)

If you are connected and want link metrics (RX/TX), use:

```bash
go run ./cmd/server --if wlp0s20f3 --mode link
```

## Notes

- `iw dev <if> scan` often requires elevated permissions (CAP_NET_ADMIN or sudo).
- Scan mode is the default; use `--mode link` for the previous behavior.
- If you run the binary from outside the repo and see 404s, set `WIFI_RADAR_STATIC_DIR` to the `web/static` folder.

## Endpoints

- `GET /api/status`
- `GET /api/best`
- `GET /api/stream` (SSE)
