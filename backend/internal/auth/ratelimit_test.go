package auth

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsCapacityThenDenies(t *testing.T) {
	l := NewLoginRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("ip-a") {
			t.Errorf("attempt %d denied; want allowed", i+1)
		}
	}
	if l.Allow("ip-a") {
		t.Error("4th attempt should be denied")
	}
}

func TestRateLimiterIsolatesKeys(t *testing.T) {
	l := NewLoginRateLimiter(2, time.Minute)
	if !l.Allow("ip-a") || !l.Allow("ip-a") {
		t.Fatal("ip-a setup")
	}
	if l.Allow("ip-a") {
		t.Error("ip-a should be exhausted")
	}
	if !l.Allow("ip-b") {
		t.Error("ip-b should still have its full bucket")
	}
}
