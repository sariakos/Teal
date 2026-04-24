package compose

import (
	"testing"
)

func TestDiscoverEnvVars_BracedInterpolation(t *testing.T) {
	yaml := `services:
  app:
    image: my/app:${IMAGE_TAG:-latest}
    environment:
      DATABASE_URL: ${DATABASE_URL}
      LOG_LEVEL: ${LOG_LEVEL:-info}
`
	got, err := DiscoverEnvVars(yaml)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		hasDefault bool
		defaultVal string
	}{
		"IMAGE_TAG":    {true, "latest"},
		"DATABASE_URL": {false, ""},
		"LOG_LEVEL":    {true, "info"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d vars, want %d: %#v", len(got), len(want), got)
	}
	for _, ref := range got {
		w, ok := want[ref.Name]
		if !ok {
			t.Errorf("unexpected var %q", ref.Name)
			continue
		}
		if ref.HasDefault != w.hasDefault {
			t.Errorf("%s: hasDefault=%v, want %v", ref.Name, ref.HasDefault, w.hasDefault)
		}
		if ref.DefaultValue != w.defaultVal {
			t.Errorf("%s: default=%q, want %q", ref.Name, ref.DefaultValue, w.defaultVal)
		}
		if len(ref.Sources) == 0 {
			t.Errorf("%s: missing source paths", ref.Name)
		}
	}
}

func TestDiscoverEnvVars_EnvironmentBlockKeysAreReferences(t *testing.T) {
	// Even when the user doesn't write ${VAR}, declaring the key in
	// `environment:` signals "the container needs this".
	yaml := `services:
  app:
    image: my/app
    environment:
      APP_URL:           # empty value → inherits from host env
      AUTH_SECRET: ${AUTH_SECRET}
`
	got, err := DiscoverEnvVars(yaml)
	if err != nil {
		t.Fatal(err)
	}
	saw := map[string]bool{}
	for _, r := range got {
		saw[r.Name] = true
	}
	if !saw["APP_URL"] {
		t.Error("APP_URL: empty-value key not picked up")
	}
	if !saw["AUTH_SECRET"] {
		t.Error("AUTH_SECRET reference not picked up")
	}
}

func TestDiscoverEnvVars_ListFormEnvironment(t *testing.T) {
	yaml := `services:
  app:
    image: my/app
    environment:
      - APP_URL=${APP_URL}
      - NODE_ENV=production
      - INHERITED_FROM_HOST
`
	got, err := DiscoverEnvVars(yaml)
	if err != nil {
		t.Fatal(err)
	}
	saw := map[string]bool{}
	for _, r := range got {
		saw[r.Name] = true
	}
	for _, want := range []string{"APP_URL", "NODE_ENV", "INHERITED_FROM_HOST"} {
		if !saw[want] {
			t.Errorf("%s missing — discovered: %#v", want, got)
		}
	}
}

func TestDiscoverEnvVars_PlainDollarVar(t *testing.T) {
	yaml := `services:
  app:
    image: my/app
    command: sh -c "echo $HOSTNAME"
`
	got, err := DiscoverEnvVars(yaml)
	if err != nil {
		t.Fatal(err)
	}
	saw := map[string]bool{}
	for _, r := range got {
		saw[r.Name] = true
	}
	if !saw["HOSTNAME"] {
		t.Errorf("plain $HOSTNAME not picked up — got %#v", got)
	}
}

func TestDiscoverEnvVars_OutputSortedByName(t *testing.T) {
	yaml := `services:
  app:
    image: ${ZEBRA}
    environment:
      ALPHA: ${ALPHA}
      MIDDLE: ${MIDDLE}
`
	got, err := DiscoverEnvVars(yaml)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ALPHA", "MIDDLE", "ZEBRA"}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d", len(got), len(want))
	}
	for i, n := range want {
		if got[i].Name != n {
			t.Errorf("position %d: got %q, want %q", i, got[i].Name, n)
		}
	}
}

func TestDiscoverEnvVars_DedupesAcrossReferences(t *testing.T) {
	// One var (TAG) referenced twice — once in image, once in
	// environment value. Should produce a single entry whose Sources
	// covers both, and the default from the image: reference wins.
	yaml := `services:
  app:
    image: my/app:${TAG:-latest}
    command: echo $TAG
`
	got, err := DiscoverEnvVars(yaml)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d vars, want 1: %#v", len(got), got)
	}
	if got[0].Name != "TAG" {
		t.Fatalf("got %q, want TAG", got[0].Name)
	}
	if !got[0].HasDefault || got[0].DefaultValue != "latest" {
		t.Errorf("TAG should have default 'latest' (from image:), got hasDefault=%v default=%q",
			got[0].HasDefault, got[0].DefaultValue)
	}
	if len(got[0].Sources) < 2 {
		t.Errorf("expected 2+ sources, got %d", len(got[0].Sources))
	}
}
