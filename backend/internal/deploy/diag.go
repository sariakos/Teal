package deploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/sariakos/teal/backend/internal/docker"
)

// containerDiagnostic appends a short summary of the primary container's
// state to a healthcheck failure so the user-facing error is actionable
// instead of just "context deadline exceeded". Best-effort — silent on
// inspect errors; we don't want a diagnostic helper to swallow the real
// cause.
func containerDiagnostic(ctx context.Context, dock docker.Client, id string, hint string) string {
	insp, err := dock.ContainerInspect(ctx, id)
	if err != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n")
	if hint != "" {
		fmt.Fprintf(&b, "container: %s\n", hint)
	}
	fmt.Fprintf(&b, "state: %s", insp.State)
	if insp.State == "exited" {
		fmt.Fprintf(&b, " (exit code %d)", insp.ExitCode)
	}
	if insp.Health != "" {
		fmt.Fprintf(&b, ", docker health: %s", insp.Health)
	}
	b.WriteString("\n")
	switch insp.State {
	case "exited":
		b.WriteString("→ the container crashed during startup. " +
			"Check the deploy log (Deployments tab) for the container's stdout/stderr.")
	case "running":
		b.WriteString("→ the container is running but no port responded. " +
			"Likely causes: (a) the app listens on 127.0.0.1 instead of 0.0.0.0 " +
			"inside the container — bind to all interfaces; (b) it listens on a " +
			"port outside the common list — set a teal.port label or override " +
			"the port in the Routes UI.")
	case "restarting":
		b.WriteString("→ the container is in a restart loop. " +
			"Check the deploy log for the crash reason.")
	default:
		b.WriteString("→ the container never reached \"running\". " +
			"Check depends_on healthchecks (e.g. database) — they may be timing out.")
	}
	return b.String()
}

// primaryContainerName picks the most useful name for the diagnostic
// message: the container's actual docker name when the early inspect
// succeeded, else a synthesised "<slug>-<color>-<service>-1" guess that
// matches docker compose's default naming.
func primaryContainerName(insp docker.ContainerInspect, slug, color, service string) string {
	if insp.Name != "" {
		return strings.TrimPrefix(insp.Name, "/")
	}
	if service == "" {
		return ""
	}
	return fmt.Sprintf("%s-%s-%s-1", slug, color, service)
}
