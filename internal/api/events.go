package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const sseWriteTimeout = 10 * time.Second

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	subscription, ok := s.broker.TrySubscribe(64, 128)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "SSE_CAPACITY_REACHED", "The live-event subscriber limit has been reached.")
		return
	}
	defer subscription.Close()
	headers := w.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache, no-store")
	headers.Set("Connection", "keep-alive")
	headers.Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	if err := writeSSEFrame(w, controller, ": connected\n\n"); err != nil {
		writeError(w, r, http.StatusInternalServerError, "SSE_UNSUPPORTED", "Streaming is unavailable.")
		return
	}
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-subscription.C:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			frame := fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, data)
			if writeSSEFrame(w, controller, frame) != nil {
				return
			}
		case now := <-heartbeat.C:
			if writeSSEFrame(w, controller, fmt.Sprintf(": heartbeat %s\n\n", now.UTC().Format(time.RFC3339))) != nil {
				return
			}
		}
	}
}

func writeSSEFrame(w http.ResponseWriter, controller *http.ResponseController, frame string) error {
	if err := controller.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil {
		return err
	}
	defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
	if _, err := io.WriteString(w, frame); err != nil {
		return err
	}
	return controller.Flush()
}
