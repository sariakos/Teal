package deploy

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"
)

// CommonHTTPPorts is the shortlist Teal probes when the user's compose
// doesn't tell us which port the service listens on. Order matters —
// the first port that accepts a TCP connection wins.
//
// Coverage notes:
//   80   nginx, traefik, generic HTTP servers
//   3000 Node.js convention (Express, Next.js, Remix), Grafana
//   8000 Python convention (Django, FastAPI, http.server)
//   8080 Tomcat, Spring Boot, generic-alt-HTTP
//   5000 Flask default, .NET Core
//   4000 Phoenix, Hexo, MkDocs
//   5173 Vite dev/preview default
//   8888 Jupyter, generic dev servers
//   9000 Portainer, SonarQube, php-fpm
var CommonHTTPPorts = []int{80, 3000, 8000, 8080, 5000, 4000, 5173, 8888, 9000}

// ProbeHTTPPort dials each port in candidates against ip and returns
// the first port that accepts a TCP connection within perPortTimeout.
// Returns 0 + a descriptive error when none answer — caller should
// surface this in the deploy log so the user knows to set a port
// explicitly.
//
// We don't speak HTTP; just a TCP SYN/ACK is enough to know something
// is listening. False positives are possible (a sidecar listening on
// 8080 that isn't the user's app) but rare in practice for the
// curated CommonHTTPPorts list.
func ProbeHTTPPort(ctx context.Context, ip string, candidates []int, perPortTimeout time.Duration) (int, error) {
	if perPortTimeout <= 0 {
		perPortTimeout = 500 * time.Millisecond
	}
	if ip == "" {
		return 0, fmt.Errorf("portprobe: container IP is empty")
	}
	dialer := net.Dialer{Timeout: perPortTimeout}
	for _, p := range candidates {
		dctx, cancel := context.WithTimeout(ctx, perPortTimeout)
		conn, err := dialer.DialContext(dctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(p)))
		cancel()
		if err == nil {
			_ = conn.Close()
			return p, nil
		}
	}
	return 0, fmt.Errorf("no listening HTTP port detected on %s — tried %v. "+
		"Set teal.port label or expose the right port in your compose", ip, candidates)
}

// detectPort returns the port to route traffic to for a service. It
// honours an explicit hint first (from the compose's ports: directive
// or a teal.port label), then falls back to probing CommonHTTPPorts.
// Either way it logs what it picked.
func detectPort(ctx context.Context, ip string, hint int, logf func(format string, a ...any)) (int, error) {
	if hint > 0 {
		// Verify the hint actually responds — saves a confusing 504
		// when the user typed the wrong port.
		dialer := net.Dialer{Timeout: 500 * time.Millisecond}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(hint)))
		if err == nil {
			_ = conn.Close()
			logf("[info] using compose-declared port %d on %s\n", hint, ip)
			return hint, nil
		}
		logf("[warn] compose declared port %d but it isn't listening on %s yet (%v). probing common ports…\n",
			hint, ip, err)
	}
	port, err := ProbeHTTPPort(ctx, ip, CommonHTTPPorts, 500*time.Millisecond)
	if err != nil {
		return 0, err
	}
	logf("[info] auto-detected listening port %d on %s\n", port, ip)
	return port, nil
}
