package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"wifi-radar/internal/collector"
	"wifi-radar/internal/model"
	"wifi-radar/internal/score"
	"wifi-radar/internal/store"
)

type NetworkScanner interface {
	ListNetworks() ([]model.Sample, error)
	CurrentTarget() collector.ScanTarget
	SetTarget(collector.ScanTarget)
}

type API struct {
	Store   *store.Store
	Scanner NetworkScanner
}

func (a API) Status(w http.ResponseWriter, r *http.Request) {
	status := a.Store.LatestStatus()
	writeJSON(w, status)
}

func (a API) Best(w http.ResponseWriter, r *http.Request) {
	samples := a.Store.SmoothedSamples()
	if len(samples) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	best := model.Best{Sample: samples[0], Score: score.SampleScore(samples[0])}
	for _, s := range samples[1:] {
		scoreVal := score.SampleScore(s)
		if scoreVal > best.Score {
			best = model.Best{Sample: s, Score: scoreVal}
		}
	}
	writeJSON(w, best)
}

func (a API) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := a.Store.Subscribe()
	defer a.Store.Unsubscribe(ch)

	ctx := r.Context()
	ping := time.NewTicker(10 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case status := <-ch:
			payload, _ := json.Marshal(status)
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (a API) Networks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if a.Scanner == nil {
		http.NotFound(w, r)
		return
	}

	networks, err := a.Scanner.ListNetworks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sort.Slice(networks, func(i, j int) bool {
		if networks[i].SignalDBM == networks[j].SignalDBM {
			if networks[i].SSID == networks[j].SSID {
				return networks[i].BSSID < networks[j].BSSID
			}
			return networks[i].SSID < networks[j].SSID
		}
		return networks[i].SignalDBM > networks[j].SignalDBM
	})
	writeJSON(w, struct {
		Networks []model.Sample `json:"networks"`
	}{Networks: networks})
}

func (a API) Target(w http.ResponseWriter, r *http.Request) {
	if a.Scanner == nil {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeTarget(w, a.Scanner.CurrentTarget())
	case http.MethodPost:
		var target collector.ScanTarget
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if err := dec.Decode(&target); err != nil {
			http.Error(w, "invalid target payload", http.StatusBadRequest)
			return
		}
		a.Scanner.SetTarget(target)
		writeTarget(w, a.Scanner.CurrentTarget())
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeTarget(w http.ResponseWriter, target collector.ScanTarget) {
	writeJSON(w, struct {
		Target collector.ScanTarget `json:"target"`
	}{Target: target})
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", joinMethods(methods))
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func joinMethods(methods []string) string {
	if len(methods) == 0 {
		return ""
	}
	out := methods[0]
	for _, method := range methods[1:] {
		out += ", " + method
	}
	return out
}
