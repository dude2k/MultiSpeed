// Package events implements the bounded in-process Server-Sent Event broker.
package events

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

type Event struct {
	ID        uint64          `json:"id"`
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type Subscription struct {
	C      <-chan Event
	cancel func()
}

func (s Subscription) Close() { s.cancel() }

type Broker struct {
	mu          sync.RWMutex
	subscribers map[uint64]chan Event
	nextSubID   atomic.Uint64
	nextEventID atomic.Uint64
	closed      bool
}

func New() *Broker { return &Broker{subscribers: make(map[uint64]chan Event)} }

func (b *Broker) Publish(eventType string, payload any) Event {
	data, err := json.Marshal(payload)
	if err != nil {
		data = json.RawMessage(`{"message":"event payload could not be encoded"}`)
	}
	event := Event{ID: b.nextEventID.Add(1), Type: eventType, Timestamp: time.Now().UTC(), Data: data}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return event
	}
	for id, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
			// Disconnect a slow consumer instead of silently creating an
			// undetectable gap. EventSource reconnects, and the frontend
			// reconciles every REST-backed query when the stream reopens.
			delete(b.subscribers, id)
			close(subscriber)
		}
	}
	return event
}

func (b *Broker) Subscribe(buffer int) Subscription {
	subscription, _ := b.TrySubscribe(buffer, 0)
	return subscription
}

// TrySubscribe applies an optional process-wide subscriber limit. A zero
// limit keeps the original unbounded administrative behavior for tests.
func (b *Broker) TrySubscribe(buffer, limit int) (Subscription, bool) {
	if buffer < 1 {
		buffer = 32
	}
	if buffer > 256 {
		buffer = 256
	}
	id := b.nextSubID.Add(1)
	channel := make(chan Event, buffer)
	b.mu.Lock()
	if b.closed {
		close(channel)
	} else if limit > 0 && len(b.subscribers) >= limit {
		close(channel)
		b.mu.Unlock()
		return Subscription{C: channel, cancel: func() {}}, false
	} else {
		b.subscribers[id] = channel
	}
	b.mu.Unlock()
	var once sync.Once
	return Subscription{C: channel, cancel: func() {
		once.Do(func() {
			b.mu.Lock()
			if existing, ok := b.subscribers[id]; ok {
				delete(b.subscribers, id)
				close(existing)
			}
			b.mu.Unlock()
		})
	}}, true
}

func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for id, subscriber := range b.subscribers {
		delete(b.subscribers, id)
		close(subscriber)
	}
}
