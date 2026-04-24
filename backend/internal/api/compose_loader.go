package api

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// loadEffectiveCompose returns the YAML to parse for a given app, plus
// a "source" tag the UI surfaces ("checkout", "stored", "none"). Tries
// the most recent deployment's checkout first (closest to what's
// actually live); falls back to the stored ComposeFile (paste-compose
// apps that haven't deployed yet).
//
// Shared by services + required-envvars handlers — both need the same
// "what compose is live" view of an app.
func loadEffectiveCompose(workdirRoot, slug, storedYAML string) (yaml string, source string, err error) {
	if workdirRoot != "" {
		dir := filepath.Join(workdirRoot, "deploys", slug)
		entries, rderr := os.ReadDir(dir)
		if rderr == nil {
			ids := make([]int, 0, len(entries))
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				if id, perr := strconv.Atoi(e.Name()); perr == nil {
					ids = append(ids, id)
				}
			}
			sort.Sort(sort.Reverse(sort.IntSlice(ids)))
			for _, id := range ids {
				candidates := []string{
					filepath.Join(dir, strconv.Itoa(id), "checkout", "docker-compose.yml"),
					filepath.Join(dir, strconv.Itoa(id), "checkout", "compose.yml"),
				}
				for _, c := range candidates {
					if data, rerr := os.ReadFile(c); rerr == nil {
						return string(data), "checkout", nil
					}
				}
			}
		}
	}
	if storedYAML != "" {
		return storedYAML, "stored", nil
	}
	return "", "none", nil
}
