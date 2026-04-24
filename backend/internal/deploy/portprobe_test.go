package deploy

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// startListener opens a TCP listener on a free port and returns the
// chosen port. Used to fake "an app is listening here" without
// spinning up a container.
func startListener(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	_, portStr, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return port
}

func TestProbeHTTPPortReturnsFirstResponder(t *testing.T) {
	port := startListener(t)
	// Probe a list where the responder is in the middle.
	candidates := []int{1, 2, port, 3}
	got, err := ProbeHTTPPort(context.Background(), "127.0.0.1", candidates, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("ProbeHTTPPort: %v", err)
	}
	if got != port {
		t.Errorf("got %d, want %d", got, port)
	}
}

func TestProbeHTTPPortReturnsErrorWhenNobodyAnswers(t *testing.T) {
	// All ports unlikely to be in use locally.
	_, err := ProbeHTTPPort(context.Background(), "127.0.0.1",
		[]int{1, 2, 3}, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected error when no port answers")
	}
	if !strings.Contains(err.Error(), "no listening HTTP port") {
		t.Errorf("error message should hint at the cause: %v", err)
	}
}

func TestDetectPortPrefersHintWhenItAnswers(t *testing.T) {
	hint := startListener(t)
	got, err := detectPort(context.Background(), "127.0.0.1", hint, func(string, ...any) {})
	if err != nil {
		t.Fatalf("detectPort: %v", err)
	}
	if got != hint {
		t.Errorf("got %d, want hint %d", got, hint)
	}
}

func TestDetectPortFallsBackToProbeWhenHintFails(t *testing.T) {
	// Hint is a port nobody listens on; one of the CommonHTTPPorts
	// might actually be in use on the test host (rare in CI), so we
	// can't make a strong assertion about which port comes back —
	// only that we don't return the bad hint, and that we either
	// succeed or fail with the expected error message.
	got, err := detectPort(context.Background(), "127.0.0.1", 1, func(string, ...any) {})
	if err != nil {
		if !strings.Contains(err.Error(), "no listening HTTP port") {
			t.Errorf("error message should hint at the cause: %v", err)
		}
		return
	}
	if got == 1 {
		t.Errorf("got %d (the bad hint); detectPort should not return a non-responder", got)
	}
}
