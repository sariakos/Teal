package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AuthKind selects how clone authenticates. Maps 1:1 to domain.GitAuthKind
// but kept as its own type so this package has no dependency on domain.
type AuthKind string

const (
	AuthNone AuthKind = ""    // public repo
	AuthSSH  AuthKind = "ssh" // private SSH key (PEM)
	AuthPAT  AuthKind = "pat" // personal access token (raw)
)

// Auth carries credentials in plaintext. Callers (the engine) decrypt via
// internal/crypto and pass them in here. The struct is short-lived: the
// Clone call writes the credential to a temp file and removes it before
// returning.
type Auth struct {
	Kind       AuthKind
	Credential []byte // private key PEM (SSH) or token (PAT). Empty for AuthNone.
}

// Result describes a successful clone.
type Result struct {
	CommitSHA string // resolved HEAD after clone
}

// CloneOptions bundles everything Shallow needs.
type CloneOptions struct {
	URL    string
	Branch string  // required; we always clone a specific branch shallow
	Dest   string  // empty existing-or-creatable directory
	Auth   Auth
}

// Shallow performs `git clone --depth 1 --branch <branch> <url> <dest>`
// with the appropriate auth glue. On success, the destination contains the
// repository at the tip of the requested branch and Result.CommitSHA is
// the resolved HEAD.
//
// SSH auth: writes the private key to a per-clone temp file with 0600
// permissions and sets GIT_SSH_COMMAND. The temp file is removed before
// return.
//
// PAT auth: rewrites an `https://host/path` URL into
// `https://x-access-token:<token>@host/path`. Other schemes are rejected.
//
// AuthNone: URL passed through.
func Shallow(ctx context.Context, opts CloneOptions) (Result, error) {
	if opts.URL == "" || opts.Branch == "" || opts.Dest == "" {
		return Result{}, errors.New("git: URL, Branch, and Dest are required")
	}
	if err := os.MkdirAll(filepath.Dir(opts.Dest), 0o700); err != nil {
		return Result{}, fmt.Errorf("git: ensure parent dir: %w", err)
	}

	cloneURL := opts.URL
	env := os.Environ()
	cleanup := func() {}

	switch opts.Auth.Kind {
	case AuthNone:
		// nothing to do
	case AuthPAT:
		if len(opts.Auth.Credential) == 0 {
			return Result{}, errors.New("git: PAT auth requires a credential")
		}
		rewritten, err := injectPATToken(opts.URL, string(opts.Auth.Credential))
		if err != nil {
			return Result{}, err
		}
		cloneURL = rewritten
	case AuthSSH:
		if len(opts.Auth.Credential) == 0 {
			return Result{}, errors.New("git: SSH auth requires a private key")
		}
		keyPath, c, err := writeTempPrivateKey(opts.Auth.Credential)
		if err != nil {
			return Result{}, err
		}
		cleanup = c
		// accept-new: trust the host on first use, then strict. Per-clone
		// known_hosts file inside the dest's parent so it's discarded with
		// the workdir.
		knownHosts := filepath.Join(filepath.Dir(opts.Dest), ".known_hosts")
		env = append(env,
			"GIT_SSH_COMMAND=ssh -i "+keyPath+
				" -o StrictHostKeyChecking=accept-new"+
				" -o IdentitiesOnly=yes"+
				" -o UserKnownHostsFile="+knownHosts,
		)
	default:
		return Result{}, fmt.Errorf("git: unknown auth kind %q", opts.Auth.Kind)
	}
	defer cleanup()

	args := []string{"clone", "--depth", "1", "--branch", opts.Branch, "--single-branch", cloneURL, opts.Dest}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Don't echo cloneURL on error — the PAT path embeds the token.
		return Result{}, fmt.Errorf("git: clone %s @ %s failed: %s", redactURL(opts.URL), opts.Branch, strings.TrimSpace(stderr.String()))
	}

	sha, err := resolveHEAD(ctx, opts.Dest)
	if err != nil {
		return Result{}, err
	}
	return Result{CommitSHA: sha}, nil
}

// resolveHEAD runs `git rev-parse HEAD` in the cloned directory.
func resolveHEAD(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git: rev-parse HEAD: %s", strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// writeTempPrivateKey writes pem to a 0600 file under os.TempDir() and
// returns its path plus a cleanup func.
func writeTempPrivateKey(pem []byte) (string, func(), error) {
	f, err := os.CreateTemp("", "teal-deploy-key-*.pem")
	if err != nil {
		return "", nil, fmt.Errorf("git: temp key: %w", err)
	}
	if _, err := f.Write(pem); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("git: write key: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("git: chmod key: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", nil, err
	}
	return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
}

// injectPATToken rewrites an https URL to embed a PAT as
// `x-access-token:<token>` userinfo. Other schemes are rejected — git PATs
// only make sense over HTTPS.
func injectPATToken(rawURL, token string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("git: parse URL: %w", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("git: PAT auth requires https URL, got %s", u.Scheme)
	}
	u.User = url.UserPassword("x-access-token", token)
	return u.String(), nil
}

// RedactURL strips userinfo so error messages don't leak credentials. Used
// before printing a URL when the original might have been rewritten.
func RedactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid url>"
	}
	u.User = nil
	return u.String()
}

// redactURL is the lowercase alias used inside the package (and tests).
func redactURL(rawURL string) string { return RedactURL(rawURL) }
