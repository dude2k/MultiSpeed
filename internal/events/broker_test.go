package events

import "testing"

func TestBrokerDisconnectsSlowSubscriberAndReleasesCapacity(t *testing.T) {
	broker := New()
	t.Cleanup(broker.Close)
	subscription, ok := broker.TrySubscribe(1, 1)
	if !ok {
		t.Fatal("first subscriber was rejected")
	}
	broker.Publish("first", map[string]any{"value": 1})
	broker.Publish("second", map[string]any{"value": 2})

	if event, channelOpen := <-subscription.C; !channelOpen || event.Type != "first" {
		t.Fatalf("buffered event=(%q, %t), want first event before close", event.Type, channelOpen)
	}
	if _, channelOpen := <-subscription.C; channelOpen {
		t.Fatal("slow subscriber remained connected after its buffer overflowed")
	}

	replacement, ok := broker.TrySubscribe(1, 1)
	if !ok {
		t.Fatal("overflowed subscriber did not release global capacity")
	}
	replacement.Close()
	subscription.Close()
}
