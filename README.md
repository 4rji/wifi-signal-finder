# WiFi Radar (Go)

Minimal MVP that scans Wi-Fi signal levels via `iw`, exposes an HTTP API + SSE, and renders a small radar/gauge UI.

![WiFi Radar UI](image.webp)

## Run (scan mode, default)

```bash
go run .

or

go run . scan -i wlp0s20f3 --interval 500ms --listen 0.0.0.0:8888
```

When run without a function, the CLI shows a menu so you can choose `scan` or `metrics`. In scan mode, it scans available networks and prompts you to pick one. The app then keeps scanning and tracks that network's RSSI without connecting. RX/TX rates are not available in scan mode.

The CLI prints the available functions at startup. You can also pass a function directly, such as `go run . scan` or `go run . metrics`. The aliases `metric`, `metrix`, and `link` also start metrics mode. Use `--help` to show the same function list with all flags.

You can skip the prompt:

```bash
go run . scan -i wlp0s20f3 --ssid "MyWiFi"
```

or:

```bash
go run . scan -i wlp0s20f3 --bssid aa:bb:cc:dd:ee:ff
```

## Run (link mode)

If you are connected and want link metrics (RX/TX), use:

```bash
go run . metrics -i wlp0s20f3
```

## Raspberry Pi 2.4 inch display

Use `-rb` to serve the compact Raspberry display UI. It replaces the desktop dashboard at `/` with a small screen that only shows the spinning radar wheel, current dBm, quality, SSID, BSSID, interface, and frequency.

Build for Raspberry Pi 2:

```bash
GOOS=linux GOARCH=arm GOARM=7 go build -o wifi-radar .
```

Run it on the Pi:

```bash
sudo ./wifi-radar scan -i wlan0 --ssid "MyWiFi" -rb --listen 127.0.0.1:8888 --open=false
```

Open the 2.4 inch screen in Chromium kiosk:

```bash
chromium-browser --kiosk --disable-infobars --noerrdialogs http://127.0.0.1:8888/
```

Or with Firefox:

```bash
firefox --kiosk http://127.0.0.1:8888/
```

If you run with `-rb` and leave `--open=true`, the app tries to launch Chromium in kiosk mode automatically, then Firefox in kiosk mode, then falls back to xdg-open. Run services with `--open=false`; browser autostart needs a graphical session.

### Start at boot

Run the scanner as a system service:

```ini
[Unit]
Description=WiFi Radar
After=network.target

[Service]
ExecStart=/home/pi/wifi-radar scan -i wlan0 --ssid=MyWiFi -rb --listen 127.0.0.1:8888 --open=false
Restart=always
User=root

[Install]
WantedBy=multi-user.target
```

Save that as `/etc/systemd/system/wifi-radar.service`, then run:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now wifi-radar.service
```

Start the kiosk browser from the desktop session, not from the system service. Create `/home/pi/.config/autostart/wifi-radar-kiosk.desktop`:

```ini
[Desktop Entry]
Type=Application
Name=WiFi Radar Kiosk
Exec=sh -c 'sleep 5; firefox --kiosk http://127.0.0.1:8888/'
X-GNOME-Autostart-enabled=true
```

If the Pi boots to console only, enable desktop autologin:

```bash
sudo raspi-config nonint do_boot_behaviour B4
sudo reboot
```

`Error: no DISPLAY environment variable specified` means the browser was started without a graphical desktop/X session. If you installed Raspberry Pi OS Lite, install a desktop or a minimal X session before using kiosk mode.

## Notes

- `iw dev <if> scan` often requires elevated permissions (CAP_NET_ADMIN or sudo).
- Use `metrics` or `--mode link` for the previous link-metrics behavior.
- Compiled binaries include the web UI assets. Set `WIFI_RADAR_STATIC_DIR` to `web/static` only when you want to override them during development.

## Endpoints

- `GET /api/status`
- `GET /api/best`
- `GET /api/stream` (SSE)
