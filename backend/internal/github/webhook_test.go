package github

import (
	"errors"
	"testing"
)

func TestParsePushAndBranch(t *testing.T) {
	body := []byte(`{
		"ref": "refs/heads/main",
		"head_commit": {"id": "abcdef0123456789"},
		"repository": {"full_name": "owner/repo"}
	}`)
	p, err := ParsePush(body)
	if err != nil {
		t.Fatalf("ParsePush: %v", err)
	}
	if p.Branch() != "main" {
		t.Errorf("Branch = %q, want main", p.Branch())
	}
	if p.HeadCommit.ID != "abcdef0123456789" {
		t.Errorf("HeadCommit.ID = %q", p.HeadCommit.ID)
	}
	if p.Repository.FullName != "owner/repo" {
		t.Errorf("Repository.FullName = %q", p.Repository.FullName)
	}
}

func TestParsePushReturnsErrNotAPushOnEmptyRef(t *testing.T) {
	body := []byte(`{"head_commit":{"id":"x"}}`)
	if _, err := ParsePush(body); !errors.Is(err, ErrNotAPush) {
		t.Errorf("ParsePush: want ErrNotAPush, got %v", err)
	}
}

func TestBranchEmptyForTagRef(t *testing.T) {
	p := PushEvent{Ref: "refs/tags/v1.0.0"}
	if p.Branch() != "" {
		t.Errorf("tag ref should yield empty branch; got %q", p.Branch())
	}
}
