package crypto

import (
	"bytes"
	"strings"
	"testing"
)

func TestCodecRoundTrip(t *testing.T) {
	c, err := NewCodec([]byte("some-long-enough-platform-secret-xyz"))
	if err != nil {
		t.Fatal(err)
	}
	ct, err := c.Seal("test.purpose", "app:1", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := c.Open("test.purpose", "app:1", ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, []byte("hello")) {
		t.Errorf("round-trip mismatch: %q", pt)
	}
}

func TestCodecAADMismatch(t *testing.T) {
	c, _ := NewCodec([]byte("some-long-enough-platform-secret-xyz"))
	ct, _ := c.Seal("test", "app:1", []byte("hi"))
	if _, err := c.Open("test", "app:2", ct); err == nil {
		t.Error("Open accepted wrong AAD")
	}
}

func TestCodecPurposeIsolation(t *testing.T) {
	c, _ := NewCodec([]byte("some-long-enough-platform-secret-xyz"))
	ct, _ := c.Seal("a", "x", []byte("msg"))
	if _, err := c.Open("b", "x", ct); err == nil {
		t.Error("Open accepted wrong purpose (keys should be distinct)")
	}
}

func TestCodecRejectsShortSecret(t *testing.T) {
	_, err := NewCodec([]byte("short"))
	if err == nil || !strings.Contains(err.Error(), "32") {
		t.Errorf("expected short-secret error, got %v", err)
	}
}
