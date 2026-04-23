package realtime

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func newTestHub() *Hub {
	return NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestPublishOnlyToSubscribedTopics(t *testing.T) {
	h := newTestHub()
	a := h.Subscribe([]string{"x"})
	b := h.Subscribe([]string{"y"})
	defer h.Unsubscribe(a)
	defer h.Unsubscribe(b)

	h.Publish("x", "hello")
	select {
	case e := <-a.C:
		if e.Topic != "x" || e.Data != "hello" {
			t.Errorf("a got %+v", e)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("a did not receive")
	}
	select {
	case e := <-b.C:
		t.Errorf("b should not receive: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAddRemoveTopic(t *testing.T) {
	h := newTestHub()
	s := h.Subscribe([]string{"x"})
	defer h.Unsubscribe(s)

	h.AddTopic(s, "y")
	h.Publish("y", 1)
	select {
	case e := <-s.C:
		if e.Topic != "y" {
			t.Errorf("topic: %q", e.Topic)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("did not receive after AddTopic")
	}

	h.RemoveTopic(s, "y")
	h.Publish("y", 2)
	select {
	case e := <-s.C:
		t.Errorf("should not have received: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	h := newTestHub()
	s := h.Subscribe([]string{"x"})
	h.Unsubscribe(s)

	if _, ok := <-s.C; ok {
		t.Error("expected channel closed after Unsubscribe")
	}
	// Double Unsubscribe is a no-op.
	h.Unsubscribe(s)

	// Publish after unsub: nothing happens, no panic.
	h.Publish("x", "noise")
}

func TestSlowSubscriberDropsRatherThanBlocks(t *testing.T) {
	h := newTestHub()
	s := h.Subscribe([]string{"x"})
	defer h.Unsubscribe(s)

	// Fill buffer + overflow.
	for i := 0; i < DefaultBuffer+10; i++ {
		h.Publish("x", i)
	}
	if d := s.Drops(); d != 10 {
		t.Errorf("drops = %d, want 10", d)
	}
	// Drops resets after read.
	if d := s.Drops(); d != 0 {
		t.Errorf("drops after read = %d", d)
	}
}

func TestHasSubscribers(t *testing.T) {
	h := newTestHub()
	if h.HasSubscribers("x") {
		t.Error("empty hub should have no subs")
	}
	s := h.Subscribe([]string{"x"})
	if !h.HasSubscribers("x") {
		t.Error("after subscribe should have subs")
	}
	h.Unsubscribe(s)
	if h.HasSubscribers("x") {
		t.Error("after unsubscribe should be clean")
	}
}

func TestConcurrentPublishSubscribe(t *testing.T) {
	h := newTestHub()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := h.Subscribe([]string{"x"})
			go func() {
				for range s.C {
					// drain
				}
			}()
			time.Sleep(10 * time.Millisecond)
			h.Unsubscribe(s)
		}()
	}
	for i := 0; i < 100; i++ {
		go h.Publish("x", i)
	}
	wg.Wait()
}
