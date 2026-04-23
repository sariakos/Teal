package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Workdirs computes and manages on-disk working directories for App
// deployments. Layout:
//
//	<root>/deploys/<slug>/<deployment-id>/
//	    compose.yml      transformed compose file
//	    deploy.env       env vars (encrypted-at-rest values plain here; the
//	                     directory permissions are 0700 and operators must
//	                     keep the host trusted)
//	    deploy.log       captured stdout+stderr from docker compose
//
// Per-app retention is enforced by Prune(slug, keep): the keep most recent
// deployments are kept, older ones are removed. Their DB rows remain.
type Workdirs struct {
	Root string
}

// NewWorkdirs constructs a Workdirs rooted at the given path. The directory
// is not created until first use.
func NewWorkdirs(root string) *Workdirs {
	return &Workdirs{Root: root}
}

// Path returns the per-deployment working directory path. Idempotent: does
// not create anything.
func (w *Workdirs) Path(slug string, deploymentID int64) string {
	return filepath.Join(w.Root, "deploys", slug, strconv.FormatInt(deploymentID, 10))
}

// AppPath returns the per-app directory (parent of all deployment dirs).
func (w *Workdirs) AppPath(slug string) string {
	return filepath.Join(w.Root, "deploys", slug)
}

// Create makes the working directory tree for one deployment with restrictive
// permissions and returns its path.
func (w *Workdirs) Create(slug string, deploymentID int64) (string, error) {
	p := w.Path(slug, deploymentID)
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", fmt.Errorf("workdir: create %s: %w", p, err)
	}
	return p, nil
}

// Prune removes per-deployment subdirectories beyond the keep most recent
// (sorted by numeric deployment ID descending). Returns the number of
// directories removed.
func (w *Workdirs) Prune(slug string, keep int) (int, error) {
	if keep < 0 {
		keep = 0
	}
	parent := w.AppPath(slug)
	entries, err := os.ReadDir(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("workdir: read dir: %w", err)
	}

	type dep struct {
		id   int64
		name string
	}
	var deps []dep
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil {
			continue // skip non-deployment entries (.gitkeep, scratch, etc.)
		}
		deps = append(deps, dep{id: id, name: e.Name()})
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].id > deps[j].id })

	removed := 0
	for i, d := range deps {
		if i < keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(parent, d.name)); err != nil {
			return removed, fmt.Errorf("workdir: remove %s: %w", d.name, err)
		}
		removed++
	}
	return removed, nil
}

// RemoveApp deletes the entire app working tree (used when an App is
// deleted from the platform). Idempotent.
func (w *Workdirs) RemoveApp(slug string) error {
	if !strings.ContainsAny(slug, "/.") { // refuse path traversal hints
		return os.RemoveAll(w.AppPath(slug))
	}
	return fmt.Errorf("workdir: refusing to remove app with suspect slug %q", slug)
}
