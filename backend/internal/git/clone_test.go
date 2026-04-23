package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectPATToken(t *testing.T) {
	got, err := injectPATToken("https://github.com/owner/repo.git", "tok-abc")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://x-access-token:tok-abc@github.com/owner/repo.git" {
		t.Errorf("got %q", got)
	}
}

func TestInjectPATTokenRejectsNonHTTPS(t *testing.T) {
	if _, err := injectPATToken("git@github.com:owner/repo.git", "x"); err == nil {
		t.Error("expected error for non-https URL")
	}
}

func TestRedactURLStripsCredentials(t *testing.T) {
	got := redactURL("https://x-access-token:secret@github.com/owner/repo.git")
	if strings.Contains(got, "secret") {
		t.Errorf("redactURL leaked credentials: %q", got)
	}
}

// TestShallowAgainstLocalRepo creates a local repo and clones it shallow.
// Avoids depending on network or GitHub while still exercising the real
// git binary and the resolveHEAD path.
func TestShallowAgainstLocalRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	src := t.TempDir()
	mustRun(t, src, "git", "init", "-q", "-b", "main", ".")
	mustRun(t, src, "git", "config", "user.email", "t@x")
	mustRun(t, src, "git", "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(src, "README"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, src, "git", "add", ".")
	mustRun(t, src, "git", "commit", "-q", "-m", "initial")

	dest := filepath.Join(t.TempDir(), "checkout")
	res, err := Shallow(context.Background(), CloneOptions{
		URL:    src,
		Branch: "main",
		Dest:   dest,
		Auth:   Auth{Kind: AuthNone},
	})
	if err != nil {
		t.Fatalf("Shallow: %v", err)
	}
	if len(res.CommitSHA) != 40 {
		t.Errorf("CommitSHA looks wrong: %q", res.CommitSHA)
	}
	if _, err := os.Stat(filepath.Join(dest, "README")); err != nil {
		t.Errorf("expected README in checkout: %v", err)
	}
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
