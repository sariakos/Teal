package github

import (
	"encoding/json"
	"errors"
	"strings"
)

// PushEvent is the trimmed shape of GitHub's push-event JSON body. We
// deserialise only the fields Teal actually uses so the type doesn't drift
// when GitHub adds new ones.
type PushEvent struct {
	Ref        string `json:"ref"`         // e.g. "refs/heads/main"
	HeadCommit struct {
		ID string `json:"id"` // commit SHA
	} `json:"head_commit"`
	Repository struct {
		FullName string `json:"full_name"` // "owner/repo"
	} `json:"repository"`
	// Deleted is true when the push removed the ref (deleted branch). We
	// ignore those — there's nothing to deploy.
	Deleted bool `json:"deleted"`
}

// ErrNotAPush is returned by ParsePush when the body decodes but doesn't
// look like a push event (missing ref). Use the X-GitHub-Event header to
// decide what kind of payload to expect; this is a defensive parse error.
var ErrNotAPush = errors.New("github: payload is not a push event")

// ParsePush decodes a push event body. Returns ErrNotAPush if the body
// doesn't carry a ref.
func ParsePush(body []byte) (PushEvent, error) {
	var p PushEvent
	if err := json.Unmarshal(body, &p); err != nil {
		return PushEvent{}, err
	}
	if p.Ref == "" {
		return PushEvent{}, ErrNotAPush
	}
	return p, nil
}

// Branch returns the branch name extracted from a "refs/heads/<name>" ref.
// Returns "" if the ref is not a branch (tag pushes etc).
func (p PushEvent) Branch() string {
	const prefix = "refs/heads/"
	if !strings.HasPrefix(p.Ref, prefix) {
		return ""
	}
	return strings.TrimPrefix(p.Ref, prefix)
}
