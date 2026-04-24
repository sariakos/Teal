package compose

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/sariakos/teal/backend/internal/domain"
)

// reparse round-trips the rendered YAML back into a map so tests can assert
// over structure rather than string substrings.
func reparse(t *testing.T, src string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := yaml.Unmarshal([]byte(src), &out); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	return out
}

func TestTransformImageOnlySingleService(t *testing.T) {
	in := TransformInput{
		UserYAML: `services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
`,
		AppSlug:     "myapp",
		Color:       domain.ColorBlue,
		Domains:     []string{"app.local"},
		EnvFilePath: "deploy.env",
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if out.PrimaryService != "web" {
		t.Errorf("PrimaryService = %q, want web", out.PrimaryService)
	}
	if out.PortInContainer != 80 {
		t.Errorf("PortInContainer = %d, want 80", out.PortInContainer)
	}

	doc := reparse(t, out.YAML)

	// Top-level networks.platform_proxy is external.
	nets, _ := doc["networks"].(map[string]any)
	if nets == nil {
		t.Fatal("top-level networks missing")
	}
	pn, _ := nets[PlatformNetworkAlias].(map[string]any)
	if pn["external"] != true {
		t.Errorf("platform_proxy.external = %v, want true", pn["external"])
	}

	// Service "web" has the network attached and Teal labels set.
	web := doc["services"].(map[string]any)["web"].(map[string]any)
	netList, _ := web["networks"].([]any)
	found := false
	for _, n := range netList {
		if n == PlatformNetworkAlias {
			found = true
		}
	}
	if !found {
		t.Errorf("web.networks does not include %s: %v", PlatformNetworkAlias, netList)
	}

	labels, _ := web["labels"].(map[string]any)
	if labels[LabelApp] != "myapp" {
		t.Errorf("LabelApp = %v", labels[LabelApp])
	}
	if labels[LabelColor] != "blue" {
		t.Errorf("LabelColor = %v", labels[LabelColor])
	}
	if labels[LabelRole] != RolePrimary {
		t.Errorf("LabelRole = %v", labels[LabelRole])
	}

	// Env file appended.
	envFiles, _ := web["env_file"].([]any)
	if len(envFiles) != 1 || envFiles[0] != "deploy.env" {
		t.Errorf("env_file = %v, want [deploy.env]", envFiles)
	}
}

func TestTransformStripsContainerNameAndExistingTealLabels(t *testing.T) {
	in := TransformInput{
		UserYAML: `services:
  web:
    image: nginx
    container_name: pinned-name
    ports: ["80:80"]
    labels:
      teal.app: leaked
      kept: yes
`,
		AppSlug: "x", Color: domain.ColorGreen,
		Domains: []string{"x.local"},
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	doc := reparse(t, out.YAML)
	web := doc["services"].(map[string]any)["web"].(map[string]any)

	if _, ok := web["container_name"]; ok {
		t.Error("container_name not stripped")
	}
	labels := web["labels"].(map[string]any)
	if labels["kept"] != "yes" {
		t.Errorf("user label 'kept' lost: %v", labels)
	}
	if labels[LabelApp] != "x" {
		t.Errorf("LabelApp not overridden: %v", labels)
	}
}

func TestTransformPicksLabelOverPortsHeuristic(t *testing.T) {
	// Two services have ports; one has the explicit primary label. Expect
	// the labelled one to win regardless of alphabetical order.
	in := TransformInput{
		UserYAML: `services:
  api:
    image: img-a
    ports: ["8081:80"]
  worker:
    image: img-w
    ports: ["8082:80"]
    labels:
      teal.primary: "true"
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.PrimaryService != "worker" {
		t.Errorf("PrimaryService = %q, want worker (label overrides ports heuristic)", out.PrimaryService)
	}
}

func TestTransformErrorsWhenAmbiguousPrimaryLabel(t *testing.T) {
	in := TransformInput{
		UserYAML: `services:
  a: { image: x, ports: ["80:80"], labels: { teal.primary: "true" } }
  b: { image: y, ports: ["80:80"], labels: { teal.primary: "true" } }
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
	}
	if _, err := Transform(in); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Errorf("expected ambiguous-primary error, got %v", err)
	}
}

func TestTransformErrorsWhenAmbiguousMultiServiceWithoutHints(t *testing.T) {
	// Multiple services, none has ports:, none has build:, no teal.primary
	// label. The heuristic has nothing to latch onto — users must either
	// add the label or use per-service Routes.
	in := TransformInput{
		UserYAML: `services:
  worker:
    image: bg-job
  cache:
    image: redis
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
	}
	if _, err := Transform(in); err == nil || !strings.Contains(err.Error(), "cannot determine") {
		t.Errorf("expected cannot-determine error, got %v", err)
	}
}

func TestTransformSingleServiceRoutesWithoutPorts(t *testing.T) {
	// Coolify-style compose: no ports:, no labels. A single service
	// should still route — the engine's port probe will pick the port
	// at deploy time.
	in := TransformInput{
		UserYAML: `services:
  app:
    image: my-app
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if out.PrimaryService != "app" {
		t.Errorf("PrimaryService = %q, want app", out.PrimaryService)
	}
	if out.PortInContainer != 0 {
		t.Errorf("PortInContainer = %d, want 0 (probe at runtime)", out.PortInContainer)
	}
}

func TestTransformPicksBuildServiceWhenNoPorts(t *testing.T) {
	// Multiple services, the app builds from source, support services
	// use official images. build: is the "route me" signal.
	in := TransformInput{
		UserYAML: `services:
  app:
    build: .
  postgres:
    image: postgres:16
  migrate:
    image: migrate/migrate
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if out.PrimaryService != "app" {
		t.Errorf("PrimaryService = %q, want app (the build: service)", out.PrimaryService)
	}
}

func TestTransformAllowsNoDomains(t *testing.T) {
	// Background-only app: no Domains, no routing wired. The transformation
	// should still succeed and just return PrimaryService=="".
	in := TransformInput{
		UserYAML: `services:
  worker:
    image: bg-job
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: nil,
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if out.PrimaryService != "" {
		t.Errorf("PrimaryService = %q, want empty", out.PrimaryService)
	}
	doc := reparse(t, out.YAML)
	if _, ok := doc["networks"]; ok {
		t.Error("top-level networks should not be declared when there's no routing")
	}
}

func TestTransformLabelListFormIsAccepted(t *testing.T) {
	// Compose accepts labels as a list of "K=V" strings. We must normalise
	// without losing information.
	in := TransformInput{
		UserYAML: `services:
  web:
    image: nginx
    ports: ["80:80"]
    labels:
      - foo=bar
      - baz
      - "teal.primary=false"
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	doc := reparse(t, out.YAML)
	labels := doc["services"].(map[string]any)["web"].(map[string]any)["labels"].(map[string]any)
	if labels["foo"] != "bar" || labels["baz"] != "" {
		t.Errorf("user labels not preserved: %v", labels)
	}
	if _, leaked := labels["teal.primary"]; leaked {
		t.Errorf("teal.* label leaked through: %v", labels)
	}
}

func TestTransformExtractsContainerPortFromVariousForms(t *testing.T) {
	cases := []struct {
		yaml string
		want int
	}{
		{`services: { w: { image: x, ports: ["80"] } }`, 80},
		{`services: { w: { image: x, ports: ["8080:80"] } }`, 80},
		{`services: { w: { image: x, ports: ["127.0.0.1:8080:80"] } }`, 80},
		{`services: { w: { image: x, ports: ["80/tcp"] } }`, 80},
		{`services: { w: { image: x, ports: [{ target: 80, published: 8080 }] } }`, 80},
	}
	for i, c := range cases {
		out, err := Transform(TransformInput{
			UserYAML: c.yaml, AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
		})
		if err != nil {
			t.Errorf("case %d: %v", i, err)
			continue
		}
		if out.PortInContainer != c.want {
			t.Errorf("case %d: PortInContainer = %d, want %d", i, out.PortInContainer, c.want)
		}
	}
}

func TestTransformLintEmitsWarnings(t *testing.T) {
	in := TransformInput{
		UserYAML: `services:
  web:
    image: nginx
    ports: ["80:80"]
    container_name: pinned
    network_mode: host
    pid: host
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, w := range out.Warnings {
		codes[w.Code] = true
	}
	for _, want := range []string{"container_name", "network_mode_host", "pid_host"} {
		if !codes[want] {
			t.Errorf("missing warning %q; got %v", want, codes)
		}
	}
}

func TestTransformInjectsResourceLimitsOnEveryService(t *testing.T) {
	in := TransformInput{
		UserYAML: `services:
  web:
    image: nginx
    ports: ["80:80"]
  worker:
    image: busybox
    deploy:
      resources:
        reservations:
          cpus: "0.1"
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
		EnvFilePath: "deploy.env",
		CPULimit:    "0.5",
		MemoryLimit: "256m",
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	doc := reparse(t, out.YAML)
	services, _ := doc["services"].(map[string]any)
	for name, raw := range services {
		svc := raw.(map[string]any)
		dep, _ := svc["deploy"].(map[string]any)
		if dep == nil {
			t.Errorf("service %q missing deploy block: %+v", name, svc)
			continue
		}
		res, _ := dep["resources"].(map[string]any)
		limits, _ := res["limits"].(map[string]any)
		if limits["cpus"] != "0.5" || limits["memory"] != "256m" {
			t.Errorf("service %q limits = %+v", name, limits)
		}
		// Worker service had a pre-existing reservations block — must
		// survive the merge, since we only touched limits.
		if name == "worker" {
			rsv, _ := res["reservations"].(map[string]any)
			if rsv == nil || rsv["cpus"] != "0.1" {
				t.Errorf("worker reservations clobbered: %+v", rsv)
			}
		}
	}
}

func TestTransformSkipsInjectionWhenLimitsEmpty(t *testing.T) {
	in := TransformInput{
		UserYAML: `services:
  web:
    image: nginx
    ports: ["80:80"]
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.YAML, "deploy:") {
		t.Errorf("should not have added a deploy block: %s", out.YAML)
	}
}

func TestTransformPreservesDefaultNetworkAccessForPrimary(t *testing.T) {
	// Regression: before the fix, the primary's networks list was
	// rewritten to [platform_proxy] only, dropping the implicit
	// default. The app could no longer reach sibling services
	// (postgres, redis) by name and crashed with EAI_AGAIN.
	in := TransformInput{
		UserYAML: `services:
  app:
    image: my/app:latest
    ports: ["3000"]
  postgres:
    image: postgres:16
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	doc := reparse(t, out.YAML)
	services, _ := doc["services"].(map[string]any)
	app, _ := services["app"].(map[string]any)
	rawNets, ok := app["networks"]
	if !ok {
		t.Fatal("primary service has no networks block after transform")
	}
	nets, ok := rawNets.([]any)
	if !ok {
		t.Fatalf("networks should be a slice, got %T", rawNets)
	}
	saw := map[string]bool{}
	for _, n := range nets {
		if s, ok := n.(string); ok {
			saw[s] = true
		}
	}
	if !saw["default"] {
		t.Errorf("primary service should also be on 'default' so it can reach siblings; got %v", nets)
	}
	if !saw["platform_proxy"] {
		t.Errorf("primary service must be on 'platform_proxy' for Traefik; got %v", nets)
	}
}

func TestTransformInjectsBuildArgsForServicesWithBuild(t *testing.T) {
	in := TransformInput{
		UserYAML: `services:
  app:
    build: .
  postgres:
    image: postgres:16
`,
		AppSlug:   "x",
		Color:     domain.ColorBlue,
		BuildArgs: []string{"APP_URL", "DATABASE_URL"},
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	doc := reparse(t, out.YAML)
	services, _ := doc["services"].(map[string]any)

	app, _ := services["app"].(map[string]any)
	build, _ := app["build"].(map[string]any)
	if build == nil {
		t.Fatalf("expected build: to be promoted to map form, got %#v", app["build"])
	}
	args, _ := build["args"].(map[string]any)
	if args == nil {
		t.Fatalf("expected build.args, got %#v", build)
	}
	for _, k := range []string{"APP_URL", "DATABASE_URL"} {
		if v, ok := args[k]; !ok {
			t.Errorf("build.args missing %s; got %v", k, args)
		} else if v != "${"+k+"}" {
			t.Errorf("build.args[%s] = %v, want ${%s}", k, v, k)
		}
	}

	// Image-only services must NOT grow a build block.
	pg, _ := services["postgres"].(map[string]any)
	if _, hasBuild := pg["build"]; hasBuild {
		t.Errorf("postgres should not gain a build directive: %v", pg)
	}
}

func TestTransformPreservesUserSuppliedBuildArgs(t *testing.T) {
	in := TransformInput{
		UserYAML: `services:
  app:
    build:
      context: .
      args:
        APP_URL: hardcoded-by-user
`,
		AppSlug:   "x",
		Color:     domain.ColorBlue,
		BuildArgs: []string{"APP_URL", "DATABASE_URL"},
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	doc := reparse(t, out.YAML)
	services, _ := doc["services"].(map[string]any)
	app, _ := services["app"].(map[string]any)
	build, _ := app["build"].(map[string]any)
	args, _ := build["args"].(map[string]any)
	if args["APP_URL"] != "hardcoded-by-user" {
		t.Errorf("user-supplied APP_URL got overridden: %v", args["APP_URL"])
	}
	if args["DATABASE_URL"] != "${DATABASE_URL}" {
		t.Errorf("DATABASE_URL should have been added: %v", args["DATABASE_URL"])
	}
}

func TestTransformDeclaresTopLevelDefaultNetwork(t *testing.T) {
	// Regression: declaring platform_proxy at top level without also
	// declaring `default:` caused some compose versions to skip
	// auto-creating the project default network, leaving non-primary
	// services (postgres, migrate) unable to resolve each other by
	// DNS.
	in := TransformInput{
		UserYAML: `services:
  app:
    build: .
  postgres:
    image: postgres:16
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	doc := reparse(t, out.YAML)
	topNets, ok := doc["networks"].(map[string]any)
	if !ok {
		t.Fatal("top-level networks missing")
	}
	if _, ok := topNets["default"]; !ok {
		t.Errorf("top-level networks should declare 'default'; got %v", topNets)
	}
	if _, ok := topNets["platform_proxy"]; !ok {
		t.Errorf("top-level networks should declare 'platform_proxy'; got %v", topNets)
	}
}

func TestTransformRespectsExplicitNetworksOnPrimary(t *testing.T) {
	// When the user declared their own networks: list, we add
	// platform_proxy only and DON'T sneak default back in — they
	// opted out of the default deliberately.
	in := TransformInput{
		UserYAML: `services:
  app:
    image: my/app:latest
    ports: ["3000"]
    networks: [internal]
networks:
  internal:
`,
		AppSlug: "x", Color: domain.ColorBlue, Domains: []string{"x.local"},
	}
	out, err := Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	doc := reparse(t, out.YAML)
	services, _ := doc["services"].(map[string]any)
	app, _ := services["app"].(map[string]any)
	nets, _ := app["networks"].([]any)
	saw := map[string]bool{}
	for _, n := range nets {
		if s, ok := n.(string); ok {
			saw[s] = true
		}
	}
	if saw["default"] {
		t.Errorf("user-declared networks should not have 'default' silently added; got %v", nets)
	}
	if !saw["internal"] || !saw["platform_proxy"] {
		t.Errorf("expected internal + platform_proxy; got %v", nets)
	}
}
