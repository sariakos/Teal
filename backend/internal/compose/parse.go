package compose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

// Warning is a non-blocking lint finding. The deploy engine surfaces these
// in the deploy log so users can act on them later.
type Warning struct {
	Service string // empty for compose-level findings
	Code    string // short stable identifier, e.g. "container_name"
	Message string // human-readable
}

// Document is a loosely-typed view of the parsed compose YAML. We keep it
// generic (map[string]any) rather than binding to a compose-spec struct
// because the spec has many optional fields that drift between Compose
// versions; a generic representation lets us mutate exactly what we touch
// and pass everything else through unchanged.
type Document struct {
	root map[string]any
}

// Parse loads a compose YAML string into a Document. Returns an error if the
// input is not a valid YAML mapping.
func Parse(src string) (*Document, error) {
	if strings.TrimSpace(src) == "" {
		return nil, errors.New("compose: input is empty")
	}
	var root map[string]any
	if err := yaml.Unmarshal([]byte(src), &root); err != nil {
		return nil, fmt.Errorf("compose: yaml parse: %w", err)
	}
	if root == nil {
		return nil, errors.New("compose: top level must be a mapping")
	}
	return &Document{root: root}, nil
}

// Lint inspects the document for patterns that conflict with the blue-green
// engine. Findings are returned as Warnings (non-blocking) per the spec —
// users can deploy them but should be aware of the implications.
func (d *Document) Lint() []Warning {
	var warns []Warning
	services := d.services()
	for name, svc := range services {
		if _, ok := svc["container_name"]; ok {
			warns = append(warns, Warning{
				Service: name, Code: "container_name",
				Message: "container_name forces a fixed name and will collide between blue/green stacks; we strip it during transform",
			})
		}
		if mode, ok := svc["network_mode"].(string); ok {
			if mode == "host" {
				warns = append(warns, Warning{
					Service: name, Code: "network_mode_host",
					Message: "network_mode: host bypasses the platform proxy; this service won't be routable through Teal",
				})
			}
		}
		if pid, ok := svc["pid"].(string); ok && pid == "host" {
			warns = append(warns, Warning{
				Service: name, Code: "pid_host",
				Message: "pid: host shares the host PID namespace; deploys can disrupt the host",
			})
		}
	}
	return warns
}

// Validate runs the user's YAML through `docker compose -f - config` for
// the authoritative validity check. ctx bounds how long we wait for the
// shell-out (compose validation typically completes in well under a second).
//
// Why shell-out instead of an in-process YAML schema check: the compose
// spec is large and changes; the only complete validator is docker compose
// itself. If the binary is absent the engine cannot deploy anyway, so this
// also doubles as a presence check.
func Validate(ctx context.Context, src string) error {
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", "-", "config", "--quiet")
	cmd.Stdin = strings.NewReader(src)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("compose: validate: %s", msg)
	}
	return nil
}

// services returns the (mutable) services map, creating it if absent so
// callers can write back without nil-checking.
func (d *Document) services() map[string]map[string]any {
	raw, _ := d.root["services"].(map[string]any)
	out := make(map[string]map[string]any, len(raw))
	for name, v := range raw {
		if m, ok := v.(map[string]any); ok {
			out[name] = m
		}
	}
	return out
}

// putServices writes the services map back into the document root,
// preserving any other top-level keys the user may have set (volumes,
// networks, configs, x-* extensions, etc.).
func (d *Document) putServices(services map[string]map[string]any) {
	out := make(map[string]any, len(services))
	for name, svc := range services {
		out[name] = svc
	}
	d.root["services"] = out
}
