package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"wifi-radar/internal/model"
	"wifi-radar/internal/score"
	"wifi-radar/internal/store"
)

type API struct {
	Store *store.Store
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
