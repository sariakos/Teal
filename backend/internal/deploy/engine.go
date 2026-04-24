package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/sariakos/teal/backend/internal/compose"
	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/docker"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/git"
	"github.com/sariakos/teal/backend/internal/githubapp"
	"github.com/sariakos/teal/backend/internal/store"
	"github.com/sariakos/teal/backend/internal/traefik"
)

// teeLogWriter forwards every Write to the underlying file AND splits
// the byte stream into lines, publishing each complete line on the
// realtime topic. Bytes that don't yet end in '\n' are buffered until
// the next write.
type teeLogWriter struct {
	inner io.Writer
	pub   Publisher
	topic string
	buf   bytes.Buffer
}

func (t *teeLogWriter) Write(p []byte) (int, error) {
	n, err := t.inner.Write(p)
	if t.pub != nil {
		t.buf.Write(p[:n])
		for {
			b := t.buf.Bytes()
			i := bytes.IndexByte(b, '\n')
			if i < 0 {
				break
			}
			line := string(b[:i])
			t.buf.Next(i + 1)
			t.pub.Publish(t.topic, map[string]any{
				"line": line,
				"ts":   time.Now().UTC(),
			})
		}
	}
	return n, err
}

// Phase is a fine-grained progress state for a single deployment. Coarse
// status (pending/running/succeeded/failed/canceled) lives on the
// deployment row in SQLite; Phase is in-memory and serves the live
// progress UI. We deliberately don't persist Phase: the set will grow over
// time and we don't want to schema-migrate every change.
type Phase string

const (
	PhasePending      Phase = "pending"
	PhasePulling      Phase = "pulling"
	PhaseBuilding     Phase = "building"
	PhaseStarting     Phase = "starting"
	PhaseHealthCheck  Phase = "healthcheck"
	PhaseFlipTraffic  Phase = "flipping_traffic"
	PhaseDraining     Phase = "draining"
	PhaseTearingDown  Phase = "tearing_down"
	PhaseSucceeded    Phase = "succeeded"
	PhaseFailed       Phase = "failed"
)

// DefaultRetention is the number of working directories kept per app. Older
// deployments' working dirs are pruned; their DB rows remain. Tunable via
// EngineConfig.WorkdirKeep.
const DefaultRetention = 10

// DefaultDrainPeriod is the gap between flipping Traefik to the new color
// and tearing down the old stack. Allows in-flight requests to finish on
// the old containers. Per spec §2.
const DefaultDrainPeriod = 10 * time.Second

// EngineConfig holds the tunables. Zero values fall back to documented
// defaults.
type EngineConfig struct {
	WorkdirRoot       string        // root for workdirs; required
	TraefikDynamicDir string        // dir for per-app Traefik YAMLs; required
	WorkdirKeep       int           // retention; default DefaultRetention
	DrainPeriod       time.Duration // default DefaultDrainPeriod
	HealthTimeout     time.Duration // per-deployment; default 180s

	// PlatformSecret is the master secret used to derive symmetric keys.
	// Required so the engine can decrypt git credentials at deploy time.
	// Same value as cfg.PlatformSecret in internal/config.
	PlatformSecret []byte
}

// Engine drives blue-green deploys. Construct one via New, share it across
// the process, hand it to the API layer.
type Engine struct {
	logger *slog.Logger
	store  *store.Store
	docker docker.Client
	runner *ComposeRunner
	wd     *Workdirs
	lock   *Lock
	cfg    EngineConfig
	codec  *crypto.Codec // shared with API; derives per-purpose keys
	pub        Publisher              // optional realtime fanout; nil disables
	notify     Notifier               // optional outbound notifier; nil disables
	githubApps *githubapp.TokenCache // optional GitHub App token cache; nil disables github_app auth

	mu     sync.Mutex
	phases map[int64]Phase // deployment ID -> last Phase observed
}

// Notifier is the subset of internal/notify the engine needs. Defined
// as an interface so tests can pass a no-op without dragging in the
// HTTP/SMTP machinery.
type Notifier interface {
	OnDeploymentFinished(ctx context.Context, evt NotifyEvent)
}

// NotifyEvent is a copy of internal/notify.Event, redeclared here to
// avoid an import cycle (notify imports store + crypto, both already
// imported by engine; the actual struct shape is identical).
type NotifyEvent struct {
	App        domain.App
	Deployment domain.Deployment
	Failed     bool
	Reason     string
}

// Publisher is the subset of the realtime hub the engine uses. Defined
// as an interface so tests can run with a no-op publisher.
type Publisher interface {
	Publish(topic string, data any)
}

// DeployTopic returns the realtime topic name for a deployment's phase
// + log stream. API + frontend must agree on the format.
func DeployTopic(deploymentID int64) string {
	return "deploy." + strconv.FormatInt(deploymentID, 10)
}

// New constructs an Engine. Returns an error if the platform secret cannot
// be used to derive subsystem keys.
func New(logger *slog.Logger, st *store.Store, dock docker.Client, cfg EngineConfig) (*Engine, error) {
	if cfg.WorkdirKeep == 0 {
		cfg.WorkdirKeep = DefaultRetention
	}
	if cfg.DrainPeriod == 0 {
		cfg.DrainPeriod = DefaultDrainPeriod
	}
	if cfg.HealthTimeout == 0 {
		cfg.HealthTimeout = 180 * time.Second
	}
	if len(cfg.PlatformSecret) == 0 {
		return nil, errors.New("deploy: PlatformSecret is required")
	}
	codec, err := crypto.NewCodec(cfg.PlatformSecret)
	if err != nil {
		return nil, fmt.Errorf("deploy: codec: %w", err)
	}
	return &Engine{
		logger: logger,
		store:  st,
		docker: dock,
		runner: NewComposeRunner(),
		wd:     NewWorkdirs(cfg.WorkdirRoot),
		lock:   NewLock(st.DB),
		cfg:    cfg,
		codec:  codec,
		phases: map[int64]Phase{},
	}, nil
}

// NewWithCodec is the constructor used by the API wiring when it already
// built a shared Codec. The plain New is kept for tests.
func NewWithCodec(logger *slog.Logger, st *store.Store, dock docker.Client, cfg EngineConfig, codec *crypto.Codec) *Engine {
	if cfg.WorkdirKeep == 0 {
		cfg.WorkdirKeep = DefaultRetention
	}
	if cfg.DrainPeriod == 0 {
		cfg.DrainPeriod = DefaultDrainPeriod
	}
	if cfg.HealthTimeout == 0 {
		cfg.HealthTimeout = 180 * time.Second
	}
	return &Engine{
		logger: logger,
		store:  st,
		docker: dock,
		runner: NewComposeRunner(),
		wd:     NewWorkdirs(cfg.WorkdirRoot),
		lock:   NewLock(st.DB),
		cfg:    cfg,
		codec:  codec,
		phases: map[int64]Phase{},
	}
}

// SetPublisher attaches a realtime publisher. cmd/teal calls this after
// constructing the hub. Safe to call before Run; not safe to swap
// during a deploy (the in-flight goroutine has already captured the
// reference, but reads of e.pub use a single load — accept the race).
func (e *Engine) SetPublisher(p Publisher) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pub = p
}

// SetNotifier attaches the outbound notifier (Phase 7). nil disables.
func (e *Engine) SetNotifier(n Notifier) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.notify = n
}

// SetGitHubAppTokenCache attaches the GitHub App installation-token
// cache. Required to deploy apps using GitAuthGitHubApp; absent it,
// resolveGitAuth returns a clear "not configured" error rather than
// silently failing the clone.
func (e *Engine) SetGitHubAppTokenCache(c *githubapp.TokenCache) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.githubApps = c
}

// CurrentPhase returns the most recent in-memory phase for a deployment,
// or "" if no in-flight phase is recorded (caller should fall back to the
// deployment row's Status).
func (e *Engine) CurrentPhase(deploymentID int64) Phase {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.phases[deploymentID]
}

// Trigger queues a new deployment for app and returns the pending row. The
// actual work runs in a background goroutine; the caller (typically the
// HTTP handler) returns 202 with the deployment ID.
//
// Returns ErrLocked if another deploy is already in flight for the App.
func (e *Engine) Trigger(ctx context.Context, app domain.App, triggeredByUserID *int64, commitSHA string, kind domain.TriggerKind) (domain.Deployment, error) {
	target := app.ActiveColor.Other()
	if app.ActiveColor == "" {
		// First deploy: start with blue.
		target = domain.ColorBlue
	}
	if kind == "" {
		kind = domain.TriggerManual
	}

	dep, err := e.lock.Acquire(ctx, AcquireParams{
		AppID:             app.ID,
		Color:             target,
		CommitSHA:         commitSHA,
		TriggeredByUserID: triggeredByUserID,
		TriggerKind:       kind,
	})
	if err != nil {
		return domain.Deployment{}, err
	}

	// Set app status; ignore error (the deploy itself is the source of
	// truth — status display can lag).
	_ = e.store.Apps.SetStatus(ctx, app.ID, domain.AppStatusDeploying)

	// Background goroutine. Detached from the request context so the deploy
	// outlives the HTTP handler — we use a fresh background context.
	go e.run(context.Background(), app, dep)
	return dep, nil
}

// run is the deployment goroutine. Every step emits a Phase event and
// writes to the deploy log; on any failure the new stack is torn down and
// the active color is left untouched.
func (e *Engine) run(ctx context.Context, app domain.App, dep domain.Deployment) {
	logger := e.logger.With("deployment_id", dep.ID, "app", app.Slug, "color", dep.Color)
	logger.Info("deploy starting")

	if err := e.lock.Started(ctx, dep.ID); err != nil {
		logger.Error("mark started", "err", err)
	}
	e.setPhase(dep.ID, PhasePulling) // first useful phase after pending

	workdir, err := e.wd.Create(app.Slug, dep.ID)
	if err != nil {
		e.fail(ctx, app, dep, "workdir: "+err.Error())
		return
	}
	logFileRaw, err := os.Create(filepath.Join(workdir, "deploy.log"))
	if err != nil {
		e.fail(ctx, app, dep, "open deploy.log: "+err.Error())
		return
	}
	defer logFileRaw.Close()
	// Tee writes also publish to the realtime topic so live UI subscribers
	// see the same lines that land in deploy.log.
	logFile := io.Writer(logFileRaw)
	if e.pub != nil {
		logFile = &teeLogWriter{inner: logFileRaw, pub: e.pub, topic: DeployTopic(dep.ID)}
	}

	envPath := filepath.Join(workdir, "deploy.env")
	envRes, err := hydrateEnv(ctx, e.store, e.codec, app)
	if err != nil {
		e.fail(ctx, app, dep, "hydrate env: "+err.Error())
		return
	}
	for _, w := range envRes.Warnings {
		fmt.Fprintf(logFile, "[warn] env: %s\n", w)
	}
	if err := os.WriteFile(envPath, envRes.Body, 0o600); err != nil {
		e.fail(ctx, app, dep, "write env: "+err.Error())
		return
	}
	if envRes.Hash != "" {
		dep.EnvVarSetHash = envRes.Hash
		if err := e.store.Deployments.Update(ctx, dep); err != nil {
			fmt.Fprintf(logFile, "[warn] persist env hash: %v\n", err)
		}
	}

	// Resolve the compose source: prefer git when configured, fall back to
	// the stored ComposeFile.
	//
	// projectDir matters for relative paths in the user's compose
	// (build contexts, bind-mount sources, secret/config files). For
	// git-source apps it's the checkout dir so `build: ./app` resolves
	// to <checkout>/app. For paste-compose apps there are no relative
	// paths to resolve (the workdir is the only thing on disk), so we
	// leave it empty and let docker compose default to the dir of the
	// -f file.
	composeYAML := app.ComposeFile
	projectDir := ""
	if app.GitURL != "" {
		fmt.Fprintf(logFile, "[info] cloning %s @ %s\n", redactGitURL(app.GitURL), app.EffectiveGitBranch())
		auth, err := e.resolveGitAuth(ctx, app)
		if err != nil {
			e.fail(ctx, app, dep, "git auth: "+err.Error())
			return
		}
		checkoutDir := filepath.Join(workdir, "checkout")
		res, err := git.Shallow(ctx, git.CloneOptions{
			URL:    app.GitURL,
			Branch: app.EffectiveGitBranch(),
			Dest:   checkoutDir,
			Auth:   auth,
		})
		if err != nil {
			e.fail(ctx, app, dep, "git clone: "+err.Error())
			return
		}
		fmt.Fprintf(logFile, "[info] cloned %s @ %s\n", res.CommitSHA, app.EffectiveGitBranch())
		// Update the deployment row's commit SHA from the resolved HEAD —
		// the user may have triggered with no SHA, in which case we record
		// what we actually deployed.
		dep.CommitSHA = res.CommitSHA
		if err := e.store.Deployments.Update(ctx, dep); err != nil {
			fmt.Fprintf(logFile, "[warn] persist commit SHA: %v\n", err)
		}

		composeRel := app.GitComposePath
		if composeRel == "" {
			composeRel = "docker-compose.yml"
		}
		composeBytes, err := os.ReadFile(filepath.Join(checkoutDir, composeRel))
		if err != nil {
			e.fail(ctx, app, dep, "read compose from repo ("+composeRel+"): "+err.Error())
			return
		}
		composeYAML = string(composeBytes)
		// Compose path inside the repo can live in a subdirectory
		// (e.g. "deploy/docker-compose.yml"). Anchor project-dir to
		// the directory containing the compose so `build: ../app`
		// patterns work too.
		projectDir = filepath.Dir(filepath.Join(checkoutDir, composeRel))
	}

	if composeYAML == "" {
		e.fail(ctx, app, dep, "no compose file (configure git or paste a compose)")
		return
	}

	// Domains may be empty (background-only app); compose handles that.
	domains := splitDomains(app.Domains)

	// Resolve effective routes. Per-service routes (app.Routes) take
	// precedence; if empty, fall back to the legacy single-domain
	// model where all Domains route to the heuristically-picked
	// primary service.
	routes := effectiveRoutes(app, domains)
	attachServices := uniqueServiceNames(routes)

	// EnvFilePath: when project-dir is the checkout (git case), a
	// relative "deploy.env" would resolve to <checkout>/deploy.env
	// which is inside the user's repo — wrong. Use the absolute path
	// so docker compose finds the engine-written env file no matter
	// where project-dir points.
	envFilePath := "deploy.env"
	if projectDir != "" {
		envFilePath = envPath
	}

	tx, err := compose.Transform(compose.TransformInput{
		UserYAML:       composeYAML,
		AppSlug:        app.Slug,
		Color:          dep.Color,
		Domains:        domains,
		EnvFilePath:    envFilePath,
		CPULimit:       app.CPULimit,
		MemoryLimit:    app.MemoryLimit,
		AttachServices: attachServices,
	})
	if err != nil {
		e.fail(ctx, app, dep, "transform: "+err.Error())
		return
	}
	for _, w := range tx.Warnings {
		fmt.Fprintf(logFile, "[warn] %s: %s — %s\n", w.Service, w.Code, w.Message)
	}
	composePath, err := compose.WriteFile(workdir, tx.YAML)
	if err != nil {
		e.fail(ctx, app, dep, "write compose: "+err.Error())
		return
	}

	project := composeProjectName(app.Slug, dep.Color)
	composeOpts := ComposeOptions{
		Project:     project,
		ComposePath: composePath,
		ProjectDir:  projectDir,
		// Provide Teal's per-app env vars to docker compose's
		// interpolation context too — this makes `${POSTGRES_PASSWORD}`
		// patterns in the user's compose resolve from the same
		// deploy.env that env_file: would inject at runtime. Without
		// this, interpolation happens against an empty environment and
		// services that depend on substituted values fail at startup.
		EnvFilePath: envPath,
	}

	// Pull or build. We branch on whether the transformed YAML carries any
	// build directive — `docker compose build` is a no-op when nothing
	// declares one but `pull` would error if every service is build-only.
	if HasBuildDirective(tx.YAML) {
		e.setPhase(dep.ID, PhaseBuilding)
		if err := e.runner.Build(ctx, composeOpts, logFile); err != nil {
			e.fail(ctx, app, dep, "build: "+err.Error())
			return
		}
	} else {
		// Pull is best-effort: a private registry without auth, or a custom
		// image that lives only locally, will fail. Log the error but
		// continue — `up` will surface a real missing-image problem.
		if err := e.runner.Pull(ctx, composeOpts, logFile); err != nil {
			fmt.Fprintf(logFile, "[warn] pull failed: %v (continuing)\n", err)
		}
	}

	e.setPhase(dep.ID, PhaseStarting)
	if err := e.runner.Up(ctx, composeOpts, logFile); err != nil {
		e.fail(ctx, app, dep, "up: "+err.Error())
		_ = e.runner.Down(context.Background(), composeOpts, io.Discard)
		return
	}

	// Health check the primary container, if there is one.
	var primaryContainerID string
	if tx.PrimaryService != "" {
		e.setPhase(dep.ID, PhaseHealthCheck)
		id, err := e.runner.PrimaryContainerID(ctx, app.Slug, string(dep.Color))
		if err != nil {
			e.fail(ctx, app, dep, "find primary container: "+err.Error())
			_ = e.runner.Down(context.Background(), composeOpts, io.Discard)
			return
		}
		if id == "" {
			e.fail(ctx, app, dep, "primary container not found after up")
			_ = e.runner.Down(context.Background(), composeOpts, io.Discard)
			return
		}
		primaryContainerID = id

		// Resolve a target for the healthcheck. Three cases, picked by
		// ModeAuto via pickMode:
		//   - compose declared a port (tx.PortInContainer > 0) → pass
		//     HTTPHostPort, HTTPProbeMode polls it until 2xx/3xx/4xx.
		//   - Coolify-style (no port in compose) → pass TCPProbeIP,
		//     TCPProbeAnyMode cycles CommonHTTPPorts every tick until
		//     one accepts a connection. This tolerates slow depends_on
		//     chains (e.g. app blocked until postgres passes its own
		//     healthcheck).
		//   - container has no platform-network IP yet → fall through
		//     to RunningOnlyMode.
		hostPort := ""
		tcpProbeIP := ""
		insp, inspErr := e.docker.ContainerInspect(ctx, id)
		if inspErr == nil {
			if ip := insp.NetworkIPs[traefik.PlatformNetworkName]; ip != "" {
				if tx.PortInContainer > 0 {
					hostPort = fmt.Sprintf("%s:%d", ip, tx.PortInContainer)
				} else {
					tcpProbeIP = ip
				}
			}
		} else {
			fmt.Fprintf(logFile, "[warn] healthcheck: container inspect: %v\n", inspErr)
		}
		if err := Wait(ctx, e.docker, id, CheckConfig{
			Mode:         ModeAuto,
			Timeout:      e.cfg.HealthTimeout,
			HTTPHostPort: hostPort,
			TCPProbeIP:   tcpProbeIP,
		}); err != nil {
			e.fail(ctx, app, dep, "healthcheck: "+err.Error()+containerDiagnostic(ctx, e.docker, id, primaryContainerName(insp, app.Slug, string(dep.Color), tx.PrimaryService)))
			_ = e.runner.Down(context.Background(), composeOpts, io.Discard)
			return
		}
	}

	// Flip Traefik. Build one Traefik route per effective Route, find
	// each route's container, probe its port, write a single multi-
	// router dynconf file. For legacy single-domain apps (Routes empty
	// + Domains set), routes contains exactly one entry with the
	// primary service implied — same shape as before.
	if len(routes) > 0 && primaryContainerID != "" {
		e.setPhase(dep.ID, PhaseFlipTraffic)
		tlsEnabled, redirect, err := e.tlsFlagsFor(ctx)
		if err != nil {
			fmt.Fprintf(logFile, "[warn] read tls settings: %v (defaulting to HTTP-only)\n", err)
		}
		multi, err := e.buildMultiSpec(ctx, app, dep, routes, tx.PrimaryService, tlsEnabled, redirect, project, logFile)
		if err != nil {
			e.fail(ctx, app, dep, "build routes: "+err.Error())
			_ = e.runner.Down(context.Background(), composeOpts, io.Discard)
			return
		}
		if err := traefik.WriteMulti(e.cfg.TraefikDynamicDir, multi); err != nil {
			e.fail(ctx, app, dep, "traefik write: "+err.Error())
			_ = e.runner.Down(context.Background(), composeOpts, io.Discard)
			return
		}
	}

	// Record the new active color BEFORE tearing down the old stack — if
	// the operator restarts mid-teardown we still want the active color
	// recorded.
	if err := e.store.Apps.SetActiveColor(ctx, app.ID, dep.Color); err != nil {
		fmt.Fprintf(logFile, "[warn] set active color: %v\n", err)
	}

	// Drain.
	if app.ActiveColor != "" {
		e.setPhase(dep.ID, PhaseDraining)
		select {
		case <-ctx.Done():
		case <-time.After(e.cfg.DrainPeriod):
		}

		// Tear down old.
		e.setPhase(dep.ID, PhaseTearingDown)
		oldProject := composeProjectName(app.Slug, app.ActiveColor)
		// We use the NEW deploy's compose file to drive the tear-down;
		// `docker compose down -p <oldProject>` only cares about the
		// project name and selects containers by label, so any compose file
		// targeting the same services works. Operationally this is fine
		// because the old stack's services are the same set (Compose is
		// converging by name).
		oldOpts := ComposeOptions{Project: oldProject, ComposePath: composePath, ProjectDir: projectDir}
		if err := e.runner.Down(context.Background(), oldOpts, logFile); err != nil {
			fmt.Fprintf(logFile, "[warn] tear down old stack failed: %v\n", err)
		}
	}

	e.setPhase(dep.ID, PhaseSucceeded)
	if err := e.lock.Done(ctx, dep.ID, domain.DeploymentStatusSucceeded, ""); err != nil {
		logger.Error("mark done: ", "err", err)
	}
	_ = e.store.Apps.SetStatus(ctx, app.ID, domain.AppStatusRunning)
	if dep.CommitSHA != "" {
		if err := e.store.Apps.SetLastDeployedCommitSHA(ctx, app.ID, dep.CommitSHA); err != nil {
			fmt.Fprintf(logFile, "[warn] persist last commit SHA: %v\n", err)
		}
	}

	if removed, err := e.wd.Prune(app.Slug, e.cfg.WorkdirKeep); err != nil {
		fmt.Fprintf(logFile, "[warn] prune workdirs: %v\n", err)
	} else if removed > 0 {
		fmt.Fprintf(logFile, "pruned %d old workdir(s)\n", removed)
	}

	logger.Info("deploy succeeded")

	// Reload the deployment row so the notifier sees the latest
	// completed_at + status the lock subsystem just wrote.
	freshDep, err := e.store.Deployments.Get(ctx, dep.ID)
	if err == nil {
		dep = freshDep
	}
	e.fireNotifier(ctx, app, dep, false, "")
}

// fail records the deployment as failed and updates the App status. Does
// NOT touch Traefik or the previously active stack — failure must leave
// production untouched.
func (e *Engine) fail(ctx context.Context, app domain.App, dep domain.Deployment, reason string) {
	e.logger.Error("deploy failed", "deployment_id", dep.ID, "app", app.Slug, "reason", reason)
	e.setPhase(dep.ID, PhaseFailed)
	if err := e.lock.Done(ctx, dep.ID, domain.DeploymentStatusFailed, reason); err != nil {
		e.logger.Error("mark failed: ", "err", err)
	}
	// If the App already had an active color, the running production is
	// fine — leave its status as 'running'. Only mark 'failed' for the
	// first-ever deploy.
	if app.ActiveColor == "" {
		_ = e.store.Apps.SetStatus(ctx, app.ID, domain.AppStatusFailed)
	} else {
		_ = e.store.Apps.SetStatus(ctx, app.ID, domain.AppStatusRunning)
	}
	freshDep, err := e.store.Deployments.Get(ctx, dep.ID)
	if err == nil {
		dep = freshDep
	}
	e.fireNotifier(ctx, app, dep, true, reason)
}

// fireNotifier dispatches the post-deploy event to the notifier (when
// configured). Synchronous lookup of the notifier reference but the
// notifier itself is expected to be non-blocking (channels run their
// own goroutines per channel).
func (e *Engine) fireNotifier(ctx context.Context, app domain.App, dep domain.Deployment, failed bool, reason string) {
	e.mu.Lock()
	n := e.notify
	e.mu.Unlock()
	if n == nil {
		return
	}
	n.OnDeploymentFinished(ctx, NotifyEvent{App: app, Deployment: dep, Failed: failed, Reason: reason})
}

func (e *Engine) setPhase(deploymentID int64, p Phase) {
	e.mu.Lock()
	e.phases[deploymentID] = p
	pub := e.pub
	e.mu.Unlock()
	if pub != nil {
		pub.Publish(DeployTopic(deploymentID), map[string]any{
			"phase": string(p),
			"ts":    time.Now().UTC(),
		})
	}
}

// Rollback finds the most recent SUCCEEDED deployment for app, takes its
// compose+env from the persisted workdir, and re-runs it as a new deploy
// targeting the opposite color. Returns ErrNoRollbackCandidate if none
// exists (or its workdir was pruned).
func (e *Engine) Rollback(ctx context.Context, app domain.App, triggeredByUserID *int64) (domain.Deployment, error) {
	deps, err := e.store.Deployments.ListForApp(ctx, app.ID, 50)
	if err != nil {
		return domain.Deployment{}, err
	}
	// Skip the current active color's most recent succeeded deployment;
	// rollback means going back to the PREVIOUSLY-succeeded one.
	var candidate *domain.Deployment
	skipped := false
	for i := range deps {
		d := deps[i]
		if d.Status != domain.DeploymentStatusSucceeded {
			continue
		}
		if !skipped {
			skipped = true // skip the "current" one
			continue
		}
		candidate = &d
		break
	}
	if candidate == nil {
		return domain.Deployment{}, ErrNoRollbackCandidate
	}
	wd := e.wd.Path(app.Slug, candidate.ID)
	if _, err := os.Stat(filepath.Join(wd, "compose.yml")); err != nil {
		return domain.Deployment{}, fmt.Errorf("rollback: workdir for deployment %d is gone (pruned): %w", candidate.ID, ErrNoRollbackCandidate)
	}
	// Trigger a fresh deploy. For git-backed apps we pass the candidate's
	// commit SHA AND temporarily override GitBranch — the engine will check
	// out that exact ref. For paste-compose apps we re-deploy the same
	// stored compose (the candidate's compose lives in workdir, but
	// Trigger reads from app.ComposeFile, which hasn't changed). The
	// rollback row is tagged TriggerRollback so the UI can show "rollback".
	return e.Trigger(ctx, app, triggeredByUserID, candidate.CommitSHA, domain.TriggerRollback)
}

// ErrNoRollbackCandidate is returned by Rollback when there is no prior
// successful deployment to roll back to (or its workdir was pruned).
var ErrNoRollbackCandidate = errors.New("deploy: no rollback candidate")

// Teardown stops both colors of an App's stack and removes its Traefik
// config. Used by the API when an App is deleted.
func (e *Engine) Teardown(ctx context.Context, app domain.App) error {
	for _, c := range []domain.Color{domain.ColorBlue, domain.ColorGreen} {
		project := composeProjectName(app.Slug, c)
		// Best-effort: missing project is fine. We pass /dev/null as the
		// compose file because docker compose down -p <project> selects
		// containers by label and doesn't actually need the file.
		_ = e.runner.Down(ctx, ComposeOptions{Project: project, ComposePath: "/dev/null"}, io.Discard)
	}
	_ = traefik.Delete(e.cfg.TraefikDynamicDir, app.Slug)
	_ = e.wd.RemoveApp(app.Slug)
	return nil
}

func composeProjectName(slug string, color domain.Color) string {
	return slug + "-" + string(color)
}

// tlsFlagsFor reads the platform settings that control whether per-app
// Traefik routers attach to HTTPS. TLS is considered enabled when an ACME
// email is configured (the static config emits the websecure entrypoint
// only in that case). Errors fall through with safe defaults.
func (e *Engine) tlsFlagsFor(ctx context.Context) (tlsEnabled, redirect bool, err error) {
	email, err := e.store.PlatformSettings.GetOrDefault(ctx, domain.SettingACMEEmail, "")
	if err != nil {
		return false, false, err
	}
	if email == "" {
		return false, false, nil
	}
	// HTTPS-only by default once ACME is configured. Admins can opt out
	// by explicitly setting https.redirect_enabled=false (e.g. when an
	// HTTP-only health check sits behind a per-app router).
	rd, err := e.store.PlatformSettings.GetOrDefault(ctx, domain.SettingHTTPSRedirect, "true")
	if err != nil {
		return true, false, err
	}
	return true, rd != "false", nil
}

// resolveGitAuth decrypts the App's git credential and returns it as a
// git.Auth. AuthNone is used when GitAuthKind is empty (public repo).
// GitAuthGitHubApp short-lives an installation token via the platform
// GitHub App and hands it to the existing PAT path — git already
// handles `https://x-access-token:<token>@github.com/...` rewrites.
func (e *Engine) resolveGitAuth(ctx context.Context, app domain.App) (git.Auth, error) {
	switch app.GitAuthKind {
	case domain.GitAuthNone:
		return git.Auth{Kind: git.AuthNone}, nil
	case domain.GitAuthSSH:
		if len(app.GitAuthCredentialEncrypted) == 0 {
			return git.Auth{}, errors.New("ssh auth selected but no key stored")
		}
		pem, err := e.codec.Open("git.private_key", "app:"+strconv.FormatInt(app.ID, 10), app.GitAuthCredentialEncrypted)
		if err != nil {
			return git.Auth{}, fmt.Errorf("decrypt ssh key: %w", err)
		}
		return git.Auth{Kind: git.AuthSSH, Credential: pem}, nil
	case domain.GitAuthPAT:
		if len(app.GitAuthCredentialEncrypted) == 0 {
			return git.Auth{}, errors.New("pat auth selected but no token stored")
		}
		tok, err := e.codec.Open("git.private_key", "app:"+strconv.FormatInt(app.ID, 10), app.GitAuthCredentialEncrypted)
		if err != nil {
			return git.Auth{}, fmt.Errorf("decrypt pat: %w", err)
		}
		return git.Auth{Kind: git.AuthPAT, Credential: tok}, nil
	case domain.GitAuthGitHubApp:
		if e.githubApps == nil {
			return git.Auth{}, errors.New("github app auth selected but token cache not wired (cmd/teal didn't call SetGitHubAppTokenCache)")
		}
		if app.GitHubAppInstallationID == 0 {
			return git.Auth{}, errors.New("github app auth selected but no installation linked (open the app's Settings tab and click Install)")
		}
		cfg, err := githubapp.LoadConfig(ctx, e.store, e.codec)
		if err != nil {
			return git.Auth{}, fmt.Errorf("load github app config: %w", err)
		}
		if !cfg.Configured() {
			return git.Auth{}, errors.New("github app auth selected but the platform GitHub App is not configured (admin: /settings/github-app)")
		}
		tok, err := e.githubApps.Get(ctx, cfg, app.GitHubAppInstallationID)
		if err != nil {
			return git.Auth{}, fmt.Errorf("mint installation token: %w", err)
		}
		return git.Auth{Kind: git.AuthPAT, Credential: []byte(tok.Token)}, nil
	default:
		return git.Auth{}, fmt.Errorf("unknown git auth kind %q", app.GitAuthKind)
	}
}

// redactGitURL strips userinfo from a URL for safe logging. SSH and plain
// URLs pass through untouched.
func redactGitURL(raw string) string {
	// Reuse the same logic the git package has internally.
	return git.RedactURL(raw)
}

// splitDomains parses the comma-separated domains string into a slice,
// trimming whitespace and dropping empties. Empty input → nil.
func splitDomains(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			d := trimSpace(s[start:i])
			if d != "" {
				out = append(out, d)
			}
			start = i + 1
		}
	}
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
