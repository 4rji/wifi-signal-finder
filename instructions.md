# WiFi Radar (Go) — Codex Instructions (Linux/Arch)

This doc is a **step-by-step build plan** for an MVP:
- Go backend reads Wi-Fi link metrics (RSSI/bitrate/BSSID/SSID) from `iw`
- Backend exposes a small HTTP API + live stream (SSE)
- A lightweight web UI animates a **signal gauge / radar circle** (intensity, not true direction)

> Note: Standard Wi-Fi clients don’t provide real “direction” (AoA) without special hardware.  
> This UI shows **signal strength trends** and can be used as a “hot/cold” finder.



Target structure:

```
wifi-radar/
  cmd/server/          # main.go (HTTP server)
  internal/
    collector/         # reads iw + parses
    model/             # structs
    score/             # ranking logic
    store/             # smoothing + ring buffer
    api/               # handlers + SSE
  web/static/          # index.html + app.js + styles.css
  go.mod
  README.md
```

---

## 3) MVP design

### Data we collect (per interface)
- `ifname`
- `ssid`
- `bssid`
- `freq_mhz`
- `signal_dbm`
- `rx_bitrate_mbps`
- `tx_bitrate_mbps`
- `timestamp`

### How “best” is chosen
Simple scoring example:
- Highest priority: **signal_dbm** (closer to 0 is better)
- Then: **rx/tx bitrate**
- Then: stability (moving average)

### Backend endpoints
- `GET /api/status`  
  Returns the latest snapshot for all monitored interfaces.
- `GET /api/best`  
  Returns the “best interface” based on scoring.
- `GET /api/stream` (SSE)  
  Pushes samples continuously to the browser.

### Frontend
- Polls `GET /api/status` once at load
- Subscribes to `/api/stream`
- Animates:
  - a circle “pulse” based on RSSI
  - a gauge arc/needle based on RSSI mapped to a 0–100 scale

---

## 4) Go module init

```bash
go mod init wifi-radar
```

If you later add external packages (websocket router, etc.), `go mod tidy` will pick them up.

---

## 5) Implementation steps (recommended order)

### Step A — Collector (parse `iw dev <if> link`)
1. Run:
   ```bash
   iw dev wlp0s20f3 link
   ```
2. Parse fields:
   - `SSID:`
   - `Connected to <BSSID>`
   - `freq:`
   - `signal:`
   - `rx bitrate:`
   - `tx bitrate:`

Implementation notes:
- Use `exec.Command("iw","dev",ifname,"link").Output()`
- If not connected, `iw` usually prints `Not connected.` — handle that cleanly.
- Keep parsing conservative; whitespace varies slightly by driver.

### Step B — Store + smoothing
- Keep the latest sample per interface
- Optional: moving average (e.g., last 8 samples) so UI doesn’t jitter

### Step C — API handlers
- `/api/status` returns JSON:
  ```json
  {
    "interfaces": [
      {
        "ifname": "wlp0s20f3",
        "ssid": "Tinto",
        "bssid": "ba:fb:e4:42:11:ed",
        "freq_mhz": 5220,
        "signal_dbm": -47,
        "rx_mbps": 400.0,
        "tx_mbps": 400.0,
        "ts_unix_ms": 1735520000000
      }
    ]
  }
  ```
- `/api/stream` uses SSE:
  - `Content-Type: text/event-stream`
  - `data: <json>\n\n`

### Step D — Web UI
- `web/static/index.html` loads `app.js`
- `app.js`:
  - `new EventSource("/api/stream")`
  - update DOM + canvas SVG gauge with smoothing

---

## 6) Run commands

### Development run
From repo root:

```bash
go run ./cmd/server --if wlp0s20f3 --if wlp0s13f0u1 --interval 500ms --listen 127.0.0.1:8080
```

Then open:
- `http://127.0.0.1:8888/`

### Build a binary
```bash
go build -o wifi-radar ./cmd/server
./wifi-radar --if wlp0s20f3 --interval 500ms
```

---

## 7) Flags (suggested)

- `--if <ifname>` (repeatable): interfaces to monitor
- `--interval 500ms`: sampling interval
- `--listen 127.0.0.1:8888`: bind address
- `--public`: optional, bind `0.0.0.0` (LAN access)

---

## 8) Security / permission notes (practical)
- Reading link status via `iw dev <if> link` typically works as normal user on many systems, but some setups require elevated privileges.
- If you need it, run the server with `sudo` or give `iw` net admin capabilities.

---

## 9) “Direction” mode (what’s feasible)
If you want a “compass-like” experience **without** special hardware:
- Add a UI mode: **finder**
- User rotates laptop/adapter slowly
- App shows “hot/cold” and records **max RSSI** observed; the UI can point to “best so far” within a scan session

True direction (AoA) needs specialized hardware/driver support.

---

## 10) Next upgrade ideas
- Use nl80211 (netlink) instead of parsing `iw`
- Add scan mode (`iw dev <if> scan`) to rank nearby BSSIDs by RSSI
- Auto-switch integration (NetworkManager via D-Bus) — optional
- Export Prometheus metrics

---

## 11) What to ask Codex to generate next
Use prompts like:

- “Generate the Go backend for this layout: collector parsing `iw`, SSE stream endpoint, and static file server for `web/static`.”
- “Implement parsing of iw link output into a struct with fields: ssid, bssid, freq, signal, rx_mbps, tx_mbps.”
- “Create a minimal frontend that draws a gauge and a pulsing circle based on RSSI, updating from SSE.”

---

### Done
When you’re ready, I can generate the full codebase (Go + web UI) matching this plan.
