package collector

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"wifi-radar/internal/model"
)

var ErrNotConnected = errors.New("not connected")

type Collector struct {
	IfName string
}

func (c Collector) Collect() (model.Sample, error) {
	out, err := exec.Command("iw", "dev", c.IfName, "link").Output()
	if err != nil {
		return model.Sample{}, fmt.Errorf("iw link: %w", err)
	}

	sample, connected, err := ParseLinkOutput(out, c.IfName)
	if err != nil {
		return model.Sample{}, err
	}
	if !connected {
		return model.Sample{}, ErrNotConnected
	}
	return sample, nil
}

func ParseLinkOutput(out []byte, ifname string) (model.Sample, bool, error) {
	sample := model.Sample{IfName: ifname}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	connected := true

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.Contains(line, "Not connected") {
			connected = false
			continue
		}
		if strings.HasPrefix(line, "Connected to ") {
			bssid := strings.TrimPrefix(line, "Connected to ")
			if idx := strings.Index(bssid, " "); idx >= 0 {
				bssid = bssid[:idx]
			}
			sample.BSSID = strings.TrimSpace(bssid)
			continue
		}
		if strings.HasPrefix(line, "SSID:") {
			sample.SSID = strings.TrimSpace(strings.TrimPrefix(line, "SSID:"))
			continue
		}
		if strings.HasPrefix(line, "freq:") {
			freqStr := strings.TrimSpace(strings.TrimPrefix(line, "freq:"))
			if v, err := strconv.Atoi(freqStr); err == nil {
				sample.FreqMHz = v
			}
			continue
		}
		if strings.HasPrefix(line, "signal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if v, err := strconv.Atoi(fields[1]); err == nil {
					sample.SignalDBM = v
				}
			}
			continue
		}
		if strings.HasPrefix(line, "rx bitrate:") {
			if v, ok := parseBitrate(line); ok {
				sample.RxBitrateMbps = v
			}
			continue
		}
		if strings.HasPrefix(line, "tx bitrate:") {
			if v, ok := parseBitrate(line); ok {
				sample.TxBitrateMbps = v
			}
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return model.Sample{}, connected, fmt.Errorf("scan iw output: %w", err)
	}

	sample.TimestampUnixM = model.NowUnixMS()
	return sample, connected, nil
}

func parseBitrate(line string) (float64, bool) {
	fields := strings.Fields(line)
	for i := 0; i < len(fields)-1; i++ {
		if strings.HasSuffix(fields[i], "bitrate:") {
			if v, err := strconv.ParseFloat(fields[i+1], 64); err == nil {
				return v, true
			}
			break
		}
	}
	return 0, false
}
