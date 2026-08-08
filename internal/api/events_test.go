package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/events"
)

type deadlineTrackingWriter struct {
	header        http.Header
	body          bytes.Buffer
	deadline      time.Time
	deadlineSets  int
	writeWithout  bool
	cancelRequest context.CancelFunc
}

func (writer *deadlineTrackingWriter) Header() http.Header { return writer.header }
func (writer *deadlineTrackingWriter) WriteHeader(int)     {}
func (writer *deadlineTrackingWriter) Write(data []byte) (int, error) {
	if writer.deadline.IsZero() || time.Now().After(writer.deadline) {
		writer.writeWithout = true
	}
	return writer.body.Write(data)
}
func (writer *deadlineTrackingWriter) Flush() {
	if writer.cancelRequest != nil {
		writer.cancelRequest()
	}
}
func (writer *deadlineTrackingWriter) SetWriteDeadline(deadline time.Time) error {
	writer.deadline = deadline
	if !deadline.IsZero() {
		writer.deadlineSets++
	}
	return nil
}

func TestSSEAppliesWriteDeadlineBeforeInitialFlush(t *testing.T) {
	broker := events.New()
	t.Cleanup(broker.Close)
	ctx, cancel := context.WithCancel(context.Background())
	writer := &deadlineTrackingWriter{header: make(http.Header), cancelRequest: cancel}
	request := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/events", nil).WithContext(ctx)

	server := &Server{broker: broker}
	server.events(writer, request)

	if writer.deadlineSets == 0 || writer.writeWithout {
		t.Fatalf("deadline sets=%d writeWithoutDeadline=%t", writer.deadlineSets, writer.writeWithout)
	}
	if !writer.deadline.IsZero() {
		t.Fatal("SSE write deadline was not cleared after a successful flush")
	}
	if !strings.Contains(writer.body.String(), ": connected") {
		t.Fatalf("initial SSE frame=%q", writer.body.String())
	}
}
