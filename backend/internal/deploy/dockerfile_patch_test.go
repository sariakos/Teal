package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectArgsIntoDockerfile_BasicMultiStage(t *testing.T) {
	src := `FROM node:20 AS deps
WORKDIR /app
RUN npm ci

FROM deps AS builder
RUN npm run build

FROM node:20-alpine AS runner
CMD ["node", "server.js"]
`
	got := injectArgsIntoDockerfile(src, []string{"APP_URL", "DATABASE_URL"})
	// every FROM must be followed by ARG + ENV for both keys
	stages := strings.Count(got, "# teal-managed:")
	if stages != 3 {
		t.Errorf("expected 3 patched stages, got %d\n%s", stages, got)
	}
	if strings.Count(got, "ARG APP_URL") != 3 {
		t.Errorf("ARG APP_URL count != 3:\n%s", got)
	}
	if strings.Count(got, "ENV APP_URL=$APP_URL") != 3 {
		t.Errorf("ENV APP_URL count != 3:\n%s", got)
	}
	// User's RUN steps must still come AFTER the ARG/ENV block in
	// each stage.
	depsBlock := substringBetween(got, "FROM node:20 AS deps", "FROM deps")
	if !strings.Contains(depsBlock, "ARG APP_URL\nENV APP_URL=$APP_URL") {
		t.Errorf("ARG/ENV not in deps stage:\n%s", depsBlock)
	}
	if !strings.Contains(depsBlock, "RUN npm ci") {
		t.Errorf("user RUN missing from deps stage:\n%s", depsBlock)
	}
}

func TestInjectArgsIntoDockerfile_Idempotent(t *testing.T) {
	src := `FROM node:20
RUN npm ci
`
	once := injectArgsIntoDockerfile(src, []string{"APP_URL"})
	twice := injectArgsIntoDockerfile(once, []string{"APP_URL"})
	if once != twice {
		t.Errorf("re-patching changed output:\nfirst pass:\n%s\nsecond pass:\n%s", once, twice)
	}
}

func TestInjectArgsIntoDockerfile_NoFROMNoChange(t *testing.T) {
	src := `# scratch dockerfile with only comments
RUN echo hi
`
	got := injectArgsIntoDockerfile(src, []string{"APP_URL"})
	if got != src {
		t.Errorf("Dockerfile without FROM should be untouched:\n%s", got)
	}
}

func TestPatchDockerfile_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	df := filepath.Join(dir, "Dockerfile.prod")
	original := `FROM node:20 AS app
RUN npm ci
CMD ["node", "server.js"]
`
	if err := os.WriteFile(df, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := patchDockerfile(df, []string{"AUTH_SECRET"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(df)
	if !strings.Contains(string(got), "ARG AUTH_SECRET") {
		t.Errorf("patched file missing ARG line:\n%s", got)
	}
}

func TestPatchDockerfilesForArgs_ReadsBuildContextAndDockerfileFields(t *testing.T) {
	projectDir := t.TempDir()
	appDir := filepath.Join(projectDir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dfPath := filepath.Join(appDir, "Dockerfile.prod")
	if err := os.WriteFile(dfPath, []byte("FROM node:20\nRUN npm ci\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	services := map[string]any{
		"app": map[string]any{
			"build": map[string]any{
				"context":    "./app",
				"dockerfile": "Dockerfile.prod",
			},
		},
		"postgres": map[string]any{
			// no build — should be skipped silently
			"image": "postgres:16",
		},
		"shorthand": map[string]any{
			"build": "./app", // shorthand string form, default Dockerfile
		},
	}
	patched, err := patchDockerfilesForArgs(projectDir, services, []string{"DATABASE_URL"})
	if err != nil {
		t.Fatal(err)
	}
	if len(patched) != 1 {
		t.Errorf("expected 1 patched file (only Dockerfile.prod exists), got %v", patched)
	}
	got, _ := os.ReadFile(dfPath)
	if !strings.Contains(string(got), "ARG DATABASE_URL") {
		t.Errorf("Dockerfile.prod not patched:\n%s", got)
	}
}

// substringBetween returns the substring starting AFTER `start` and
// ending BEFORE `end`. Empty if either anchor is missing.
func substringBetween(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		return rest
	}
	return rest[:j]
}
