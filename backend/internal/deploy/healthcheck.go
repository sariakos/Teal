package deploy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/sariakos/teal/backend/internal/docker"
)

// CheckMode picks between the three readiness strategies.
type CheckMode int

const (
	// ModeAuto picks DockerHealth if the container has a healthcheck,
	// HTTPProbe if it has a port to probe, otherwise RunningOnly. The
	// engine uses this so callers don't have to make the choice explicitly.
	ModeAuto CheckMode = iota

	// DockerHealthMode polls `docker inspect` until State.Health.Status is
	// "healthy". Authoritative when the user (or image) declared a
	// healthcheck.
	DockerHealthMode

	// HTTPProbeMode does an HTTP GET against host:port at HTTPPath until
	// the response has status < 500. Used for services with a known port
	// but no docker-level healthcheck.
	HTTPProbeMode

	// RunningOnlyMode waits for State to be "running" continuously for
	// MinUpDuration. Last-resort for background workers with no signal.
	RunningOnlyMode
)

// CheckConfig configures Wait. Defaults are filled in by Wait when fields
// are zero.
type CheckConfig struct {
	Mode           CheckMode
	Timeout        time.Duration // overall deadline; default 60s
	PollInterval   time.Duration // default 1s
	MinUpDuration  time.Duration // RunningOnlyMode threshold; default 5s
	HTTPHostPort   string        // HTTPProbeMode target, e.g. "172.18.0.5:80"
	HTTPPath       string        // default "/"
	HTTPClient     *http.Client  // optional override; default 2s timeout
}

// Wait blocks until the container reaches readiness per cfg, or the context
// is cancelled, or Timeout elapses.
func Wait(ctx context.Context, dock docker.Client, containerID string, cfg CheckConfig) error {
	cfg = withDefaults(cfg)
	deadline := time.Now().Add(cfg.Timeout)
	dctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	mode := cfg.Mode
	if mode == ModeAuto {
		insp, err := dock.ContainerInspect(dctx, containerID)
		if err != nil {
			return fmt.Errorf("healthcheck: initial inspect: %w", err)
		}
		mode = pickMode(insp, cfg.HTTPHostPort)
	}

	switch mode {
	case DockerHealthMode:
		return waitDockerHealth(dctx, dock, containerID, cfg.PollInterval)
	case HTTPProbeMode:
		return waitHTTP(dctx, cfg.HTTPClient, cfg.HTTPHostPort, cfg.HTTPPath, cfg.PollInterval)
	case RunningOnlyMode:
		return waitRunning(dctx, dock, containerID, cfg.PollInterval, cfg.MinUpDuration)
	}
	return fmt.Errorf("healthcheck: unknown mode %d", mode)
}

func pickMode(insp docker.ContainerInspect, httpHostPort string) CheckMode {
	if insp.Health != "" {
		return DockerHealthMode
	}
	if httpHostPort != "" {
		return HTTPProbeMode
	}
	return RunningOnlyMode
}

func withDefaults(cfg CheckConfig) CheckConfig {
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 1 * time.Second
	}
	if cfg.MinUpDuration == 0 {
		cfg.MinUpDuration = 5 * time.Second
	}
	if cfg.HTTPPath == "" {
		cfg.HTTPPath = "/"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 2 * time.Second}
	}
	return cfg
}

func waitDockerHealth(ctx context.Context, dock docker.Client, id string, interval time.Duration) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		insp, err := dock.ContainerInspect(ctx, id)
		if err == nil {
			if insp.Health == "healthy" {
				return nil
			}
			if insp.Health == "unhealthy" {
				return errors.New("healthcheck: docker reported container unhealthy")
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("healthcheck: docker health did not pass: %w", ctx.Err())
		case <-t.C:
		}
	}
}

func waitHTTP(ctx context.Context, client *http.Client, hostPort, path string, interval time.Duration) error {
	url := "http://" + hostPort + path
	t := time.NewTicker(interval)
	defer t.Stop()
	var lastErr error
	for {
		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else if !isContextCanceled(err) {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("healthcheck: HTTP probe did not succeed (last: %v): %w", lastErr, ctx.Err())
			}
			return fmt.Errorf("healthcheck: HTTP probe did not succeed: %w", ctx.Err())
		case <-t.C:
		}
	}
}

func waitRunning(ctx context.Context, dock docker.Client, id string, interval, minUp time.Duration) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	var firstSeenRunning time.Time
	for {
		insp, err := dock.ContainerInspect(ctx, id)
		if err == nil && insp.State == "running" {
			if firstSeenRunning.IsZero() {
				firstSeenRunning = time.Now()
			}
			if time.Since(firstSeenRunning) >= minUp {
				return nil
			}
		} else {
			firstSeenRunning = time.Time{}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("healthcheck: container did not stay running for %s: %w", minUp, ctx.Err())
		case <-t.C:
		}
	}
}

// isContextCanceled detects context-driven errors from the http client so we
// don't record them as the user-facing failure cause when the deadline is
// the real reason.
func isContextCanceled(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
