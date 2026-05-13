package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wifi-radar/internal/collector"
	"wifi-radar/internal/model"
)

type fakeScanner struct {
	networks []model.Sample
	ifname   string
	target   collector.ScanTarget
}

func (f *fakeScanner) ListNetworks() ([]model.Sample, error) {
	return f.networks, nil
}

func (f *fakeScanner) CurrentInterface() string {
	return f.ifname
}

func (f *fakeScanner) SetInterface(ifname string) {
	f.ifname = ifname
	f.target = collector.ScanTarget{}
}

func (f *fakeScanner) CurrentTarget() collector.ScanTarget {
	return f.target
}

func (f *fakeScanner) SetTarget(target collector.ScanTarget) {
	f.target = target
}

func TestNetworksReturnsSortedScanResults(t *testing.T) {
	api := API{Scanner: &fakeScanner{networks: []model.Sample{
		{SSID: "Weak", BSSID: "22", SignalDBM: -80},
		{SSID: "Strong", BSSID: "11", SignalDBM: -42},
	}}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/networks", nil)
	api.Networks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload struct {
		Networks []model.Sample `json:"networks"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode networks: %v", err)
	}
	if got := payload.Networks[0].SSID; got != "Strong" {
		t.Fatalf("first network = %q, want Strong", got)
	}
}

func TestTargetUpdatesScanner(t *testing.T) {
	scanner := &fakeScanner{}
	api := API{Scanner: scanner}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/target", strings.NewReader(`{"ssid":"Lab","bssid":"aa:bb"}`))
	api.Target(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if scanner.target.SSID != "Lab" || scanner.target.BSSID != "aa:bb" {
		t.Fatalf("target = %+v, want Lab/aa:bb", scanner.target)
	}
}

func TestInterfacesUpdatesScannerAndClearsTarget(t *testing.T) {
	scanner := &fakeScanner{ifname: "wlan0", target: collector.ScanTarget{SSID: "Old"}}
	api := API{
		Scanner: scanner,
		InterfaceLister: func() ([]string, error) {
			return []string{"wlan0", "wlan1"}, nil
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/interfaces", strings.NewReader(`{"ifname":"wlan1"}`))
	api.Interfaces(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if scanner.ifname != "wlan1" {
		t.Fatalf("ifname = %q, want wlan1", scanner.ifname)
	}
	if scanner.target.SSID != "" || scanner.target.BSSID != "" {
		t.Fatalf("target = %+v, want cleared target", scanner.target)
	}
}
