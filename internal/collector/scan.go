package collector

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"wifi-radar/internal/model"
)

var ErrTargetNotFound = errors.New("target network not found")

type ScanTarget struct {
	SSID  string
	BSSID string
}

type ScanCollector struct {
	IfName  string
	Target  ScanTarget
	UseSudo bool
}

func (c *ScanCollector) Collect() (model.Sample, error) {
	networks, usedSudo, err := ScanNetworksWithFallback(c.IfName, c.UseSudo)
	if err != nil {
		return model.Sample{}, err
	}
	c.UseSudo = usedSudo

	sample, ok := PickTarget(networks, c.Target)
	if !ok {
		sample = model.Sample{
			IfName:         c.IfName,
			SSID:           c.Target.SSID,
			BSSID:          normalizeBSSID(c.Target.BSSID),
			SignalDBM:      -100,
			TimestampUnixM: model.NowUnixMS(),
		}
		return sample, ErrTargetNotFound
	}
	sample.TimestampUnixM = model.NowUnixMS()
	return sample, nil
}

func ScanNetworksWithFallback(ifname string, useSudo bool) ([]model.Sample, bool, error) {
	networks, err := ScanNetworks(ifname, useSudo)
	if err == nil {
		return networks, useSudo, nil
	}
	if !useSudo && isPermissionError(err) {
		networks, err = ScanNetworks(ifname, true)
		if err == nil {
			return networks, true, nil
		}
	}
	return nil, useSudo, err
}

func ScanNetworks(ifname string, useSudo bool) ([]model.Sample, error) {
	out, err := runIwScan(ifname, useSudo)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return nil, fmt.Errorf("iw scan: %w", err)
		}
		return nil, fmt.Errorf("iw scan: %w: %s", err, msg)
	}
	return ParseScanOutput(out, ifname)
}

func ParseScanOutput(out []byte, ifname string) ([]model.Sample, error) {
	scanner := bufio.NewScanner(bytes.NewReader(out))
	results := make([]model.Sample, 0, 16)
	var current *model.Sample
	now := model.NowUnixMS()

	flush := func() {
		if current == nil {
			return
		}
		current.TimestampUnixM = now
		results = append(results, *current)
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "BSS ") {
			flush()
			bssid := parseBSSIDLine(line)
			sample := model.Sample{
				IfName:    ifname,
				BSSID:     bssid,
				SignalDBM: -100,
			}
			current = &sample
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, "freq:") {
			freqStr := strings.TrimSpace(strings.TrimPrefix(line, "freq:"))
			if v, err := strconv.Atoi(freqStr); err == nil {
				current.FreqMHz = v
			}
			continue
		}
		if strings.HasPrefix(line, "signal:") {
			if v, ok := parseSignal(line); ok {
				current.SignalDBM = v
			}
			continue
		}
		if strings.HasPrefix(line, "SSID:") {
			current.SSID = strings.TrimSpace(strings.TrimPrefix(line, "SSID:"))
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan iw output: %w", err)
	}
	flush()
	return results, nil
}

func PickTarget(networks []model.Sample, target ScanTarget) (model.Sample, bool) {
	if len(networks) == 0 {
		return model.Sample{}, false
	}
	if target.BSSID != "" {
		want := normalizeBSSID(target.BSSID)
		for _, n := range networks {
			if normalizeBSSID(n.BSSID) == want {
				return n, true
			}
		}
		return model.Sample{}, false
	}
	if target.SSID != "" {
		found := false
		best := model.Sample{}
		for _, n := range networks {
			if n.SSID != target.SSID {
				continue
			}
			if !found || n.SignalDBM > best.SignalDBM {
				best = n
				found = true
			}
		}
		return best, found
	}
	best := networks[0]
	for _, n := range networks[1:] {
		if n.SignalDBM > best.SignalDBM {
			best = n
		}
	}
	return best, true
}

func parseBSSIDLine(line string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "BSS "))
	if idx := strings.IndexAny(rest, " \t("); idx >= 0 {
		rest = rest[:idx]
	}
	return normalizeBSSID(rest)
}

func normalizeBSSID(bssid string) string {
	return strings.ToLower(strings.TrimSpace(bssid))
}

func parseSignal(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, false
	}
	val, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, false
	}
	return int(math.Round(val)), true
}

func runIwScan(ifname string, useSudo bool) ([]byte, error) {
	args := []string{"dev", ifname, "scan"}
	if useSudo {
		cmd := exec.Command("sudo", append([]string{"iw"}, args...)...)
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr
		return cmd.Output()
	}
	return exec.Command("iw", args...).CombinedOutput()
}

func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "exit status 255") {
		return true
	}
	return strings.Contains(msg, "not permitted") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "not authorized") ||
		strings.Contains(msg, "operation not permitted")
}
