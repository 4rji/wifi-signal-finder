package store

import (
	"sync"

	"wifi-radar/internal/model"
)

type Store struct {
	mu          sync.RWMutex
	histories   map[string]*history
	subscribers map[chan model.Status]struct{}
	maxSamples  int
}

type history struct {
	samples []model.Sample
	max     int
}

func New(maxSamples int) *Store {
	if maxSamples < 1 {
		maxSamples = 1
	}
	return &Store{
		histories:   make(map[string]*history),
		subscribers: make(map[chan model.Status]struct{}),
		maxSamples:  maxSamples,
	}
}

func (s *Store) Update(sample model.Sample) {
	s.mu.Lock()
	h := s.histories[sample.IfName]
	if h == nil {
		h = &history{max: s.maxSamples}
		s.histories[sample.IfName] = h
	}
	h.add(sample)
	status := s.latestStatusLocked()
	for ch := range s.subscribers {
		select {
		case ch <- status:
		default:
		}
	}
	s.mu.Unlock()
}

func (s *Store) LatestStatus() model.Status {
	s.mu.RLock()
	status := s.latestStatusLocked()
	s.mu.RUnlock()
	return status
}

func (s *Store) SmoothedSamples() []model.Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Sample, 0, len(s.histories))
	for _, h := range s.histories {
		if len(h.samples) == 0 {
			continue
		}
		out = append(out, h.average())
	}
	return out
}

func (s *Store) Subscribe() chan model.Status {
	ch := make(chan model.Status, 4)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *Store) Unsubscribe(ch chan model.Status) {
	s.mu.Lock()
	delete(s.subscribers, ch)
	close(ch)
	s.mu.Unlock()
}

func (s *Store) latestStatusLocked() model.Status {
	status := model.Status{Interfaces: make([]model.Sample, 0, len(s.histories))}
	for _, h := range s.histories {
		if len(h.samples) == 0 {
			continue
		}
		status.Interfaces = append(status.Interfaces, h.samples[len(h.samples)-1])
	}
	return status
}

func (h *history) add(sample model.Sample) {
	h.samples = append(h.samples, sample)
	if len(h.samples) > h.max {
		h.samples = h.samples[len(h.samples)-h.max:]
	}
}

func (h *history) average() model.Sample {
	last := h.samples[len(h.samples)-1]
	if len(h.samples) == 1 {
		return last
	}

	var (
		signalTotal int
		rxTotal     float64
		txTotal     float64
	)
	for _, s := range h.samples {
		signalTotal += s.SignalDBM
		rxTotal += s.RxBitrateMbps
		txTotal += s.TxBitrateMbps
	}
	count := float64(len(h.samples))
	last.SignalDBM = int(float64(signalTotal) / count)
	last.RxBitrateMbps = rxTotal / count
	last.TxBitrateMbps = txTotal / count
	return last
}
