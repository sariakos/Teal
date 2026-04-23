// Package realtime is Teal's pub/sub fanout for WebSocket clients. One
// Hub per process; clients subscribe to typed topics and the hub
// publishes events into per-subscriber channels.
//
// Topic naming convention:
//
//   deploy.<deployment-id>           Phase events + deploy.log lines
//   containerlogs.<container-id>     Live container log frames
//   metrics.<container-id>           One sample per scrape tick
//
// Event shape on the wire is JSON: {"topic":"<topic>","data":<arbitrary>}.
// The hub does not interpret data; producers shape it.
//
// Backpressure: each subscriber has a small buffered channel (default 64).
// A subscriber that can't drain fast enough has its slowest message
// dropped — never block a producer goroutine on a slow client.
package realtime

import (
	"log/slog"
	"sync"
)

// DefaultBuffer is the per-subscriber channel size. Big enough to absorb
// burst (deploy log can spike to hundreds of lines per second during a
// build), small enough that a permanently-stuck subscriber doesn't sit
// on much memory.
const DefaultBuffer = 64

// Event is one message published to a topic. Data must be JSON-encodable.
// Producers shape Data; consumers (the WS endpoint) marshal it.
type Event struct {
	Topic string
	Data  any
}

// Subscription is the consumer's handle. Reading from C delivers events
// in publish order. A dropped event triggers Drops++ — useful for
// surfacing "you missed N events" hints in the UI.
type Subscription struct {
	C chan Event

	hub    *Hub
	id     uint64
	topics map[string]struct{}

	mu    sync.Mutex
	drops uint64
}

// Drops returns the number of events the hub had to drop because the
// subscriber's channel was full. Resets to zero on read.
func (s *Subscription) Drops() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.drops
	s.drops = 0
	return d
}

// Hub holds the subscriber registry and publishes events. Methods are
// safe for concurrent use.
type Hub struct {
	logger *slog.Logger

	mu       sync.RWMutex
	nextID   uint64
	subs     map[uint64]*Subscription            // all subscribers by ID
	byTopic  map[string]map[uint64]*Subscription // topic -> set of subscribers
}

// NewHub constructs a Hub. Pass a logger so dropped-event warnings can
// be observed; nil disables logging.
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		logger:  logger,
		subs:    map[uint64]*Subscription{},
		byTopic: map[string]map[uint64]*Subscription{},
	}
}

// Subscribe registers a new subscriber that will receive events on the
// given topics. Returns a Subscription whose C channel must be drained;
// call Unsubscribe when done.
func (h *Hub) Subscribe(topics []string) *Subscription {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	s := &Subscription{
		C:      make(chan Event, DefaultBuffer),
		hub:    h,
		id:     h.nextID,
		topics: map[string]struct{}{},
	}
	h.subs[s.id] = s
	for _, t := range topics {
		h.attachLocked(s, t)
	}
	return s
}

// AddTopic adds a topic to an existing Subscription. Idempotent.
func (h *Hub) AddTopic(s *Subscription, topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s.id]; !ok {
		return
	}
	h.attachLocked(s, topic)
}

// RemoveTopic removes a topic from an existing Subscription. Idempotent.
func (h *Hub) RemoveTopic(s *Subscription, topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(s.topics, topic)
	if set, ok := h.byTopic[topic]; ok {
		delete(set, s.id)
		if len(set) == 0 {
			delete(h.byTopic, topic)
		}
	}
}

// Unsubscribe removes the Subscription from every topic and closes its
// channel. Calling twice is a no-op.
func (h *Hub) Unsubscribe(s *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s.id]; !ok {
		return
	}
	for t := range s.topics {
		if set, ok := h.byTopic[t]; ok {
			delete(set, s.id)
			if len(set) == 0 {
				delete(h.byTopic, t)
			}
		}
	}
	delete(h.subs, s.id)
	close(s.C)
}

// Publish fans an event out to every subscriber of topic. Slow
// subscribers have their channel skipped (event dropped) rather than
// blocking the publisher.
func (h *Hub) Publish(topic string, data any) {
	h.mu.RLock()
	set := h.byTopic[topic]
	dst := make([]*Subscription, 0, len(set))
	for _, s := range set {
		dst = append(dst, s)
	}
	h.mu.RUnlock()

	evt := Event{Topic: topic, Data: data}
	for _, s := range dst {
		select {
		case s.C <- evt:
		default:
			s.mu.Lock()
			s.drops++
			d := s.drops
			s.mu.Unlock()
			h.logger.Warn("realtime: dropped event for slow subscriber",
				"topic", topic, "subscriber", s.id, "total_drops", d)
		}
	}
}

// HasSubscribers is a cheap probe used by upstream producers to skip
// expensive work (e.g. tailing a log file) when nobody is listening.
func (h *Hub) HasSubscribers(topic string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byTopic[topic]) > 0
}

// attachLocked must be called with h.mu held.
func (h *Hub) attachLocked(s *Subscription, topic string) {
	s.topics[topic] = struct{}{}
	set, ok := h.byTopic[topic]
	if !ok {
		set = map[uint64]*Subscription{}
		h.byTopic[topic] = set
	}
	set[s.id] = s
}
