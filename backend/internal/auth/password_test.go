package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashAndComparePassword(t *testing.T) {
	pw := "correct horse battery staple"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if len(hash) == 0 || strings.Contains(string(hash), pw) {
		t.Fatalf("hash looks suspect: %q", hash)
	}
	if err := ComparePassword(hash, pw); err != nil {
		t.Errorf("Compare with right password: %v", err)
	}
	if err := ComparePassword(hash, pw+"x"); !errors.Is(err, ErrPasswordMismatch) {
		t.Errorf("Compare with wrong password: want ErrPasswordMismatch, got %v", err)
	}
}

func TestHashRejectsShortPassword(t *testing.T) {
	_, err := HashPassword("short")
	if !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("want ErrPasswordTooShort, got %v", err)
	}
}
