package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadAppliesDefaultsInDev(t *testing.T) {
	t.Setenv("TEAL_ENV", "dev")
	t.Setenv("TEAL_PLATFORM_SECRET", "")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.HTTPAddr != ":3000" {
		t.Errorf("HTTPAddr = %q, want :3000", c.HTTPAddr)
	}
	if c.LogFormat != "text" {
		t.Errorf("LogFormat = %q, want text in dev", c.LogFormat)
	}
	if c.PlatformSecret == "" {
		t.Error("PlatformSecret should be auto-filled in dev")
	}
	if c.ShutdownGracePeriod != 15*time.Second {
		t.Errorf("ShutdownGracePeriod = %s", c.ShutdownGracePeriod)
	}
}

func TestLoadJSONLogFormatInProd(t *testing.T) {
	t.Setenv("TEAL_ENV", "prod")
	t.Setenv("TEAL_PLATFORM_SECRET", strings.Repeat("a", 32))

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want json in prod", c.LogFormat)
	}
}

func TestLoadRejectsProdWithoutSecret(t *testing.T) {
	t.Setenv("TEAL_ENV", "prod")
	t.Setenv("TEAL_PLATFORM_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when prod has no platform secret")
	}
	if !strings.Contains(err.Error(), "TEAL_PLATFORM_SECRET") {
		t.Errorf("error should mention TEAL_PLATFORM_SECRET: %v", err)
	}
}

func TestLoadRejectsShortSecret(t *testing.T) {
	t.Setenv("TEAL_ENV", "prod")
	t.Setenv("TEAL_PLATFORM_SECRET", "tooshort")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("expected length error, got %v", err)
	}
}

func TestLoadRejectsBypassInProd(t *testing.T) {
	t.Setenv("TEAL_ENV", "prod")
	t.Setenv("TEAL_PLATFORM_SECRET", strings.Repeat("a", 32))
	t.Setenv("TEAL_DEV_BYPASS_AUTH", "true")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "TEAL_DEV_BYPASS_AUTH") {
		t.Fatalf("expected bypass-in-prod error, got %v", err)
	}
}

func TestLoadParsesShutdownGrace(t *testing.T) {
	t.Setenv("TEAL_ENV", "dev")
	t.Setenv("TEAL_SHUTDOWN_GRACE", "5s")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ShutdownGracePeriod != 5*time.Second {
		t.Errorf("ShutdownGracePeriod = %s", c.ShutdownGracePeriod)
	}
}

func TestLoadRejectsBadDuration(t *testing.T) {
	t.Setenv("TEAL_ENV", "dev")
	t.Setenv("TEAL_SHUTDOWN_GRACE", "not-a-duration")

	_, err := Load()
	if err == nil {
		t.Fatal("expected duration parse error")
	}
}

func TestLoadRejectsUnknownEnv(t *testing.T) {
	t.Setenv("TEAL_ENV", "staging") // valid in spirit, not in this validator

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "TEAL_ENV") {
		t.Fatalf("expected env validation error, got %v", err)
	}
}
