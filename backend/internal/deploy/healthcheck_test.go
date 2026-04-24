package deploy

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/sariakos/teal/backend/internal/docker"
)

func TestWaitTCPAny_SucceedsOnFirstResponder(t *testing.T) {
	// Two candidate ports: one with a listener, one without. Confirms
	// waitTCPAny doesn't get stuck on the dead candidate.
	deadPort, liveListener := findFreePort(t), openListener(t)
	defer liveListener.Close()
	livePort := liveListener.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := waitTCPAny(ctx, "127.0.0.1", []int{deadPort, livePort}, 50*time.Millisecond); err != nil {
		t.Fatalf("waitTCPAny: %v", err)
	}
}

func TestWaitTCPAny_TimesOutWhenNothingListens(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	err := waitTCPAny(ctx, "127.0.0.1",
		[]int{findFreePort(t), findFreePort(t)},
		50*time.Millisecond)
	if err == nil {
		t.Fatal("expected deadline error, got nil")
	}
	if !strings.Contains(err.Error(), "accepted a connection") {
		t.Errorf("expected \"accepted a connection\" in error, got %q", err.Error())
	}
}

func TestPickMode_TCPProbeAnyWhenOnlyIPKnown(t *testing.T) {
	// No docker Health, no HTTPHostPort, only TCPProbeIP — should land
	// on TCPProbeAnyMode rather than RunningOnlyMode.
	if got := pickMode(docker.ContainerInspect{}, "", "172.18.0.5"); got != TCPProbeAnyMode {
		t.Errorf("pickMode = %v, want TCPProbeAnyMode", got)
	}
}

func TestPickMode_HTTPProbeWinsOverTCPProbe(t *testing.T) {
	if got := pickMode(docker.ContainerInspect{}, "172.18.0.5:3000", "172.18.0.5"); got != HTTPProbeMode {
		t.Errorf("pickMode = %v, want HTTPProbeMode", got)
	}
}

func TestPickMode_RunningOnlyWhenNoSignals(t *testing.T) {
	if got := pickMode(docker.ContainerInspect{}, "", ""); got != RunningOnlyMode {
		t.Errorf("pickMode = %v, want RunningOnlyMode", got)
	}
}

// --- test helpers ---

func findFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func openListener(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return l
}
