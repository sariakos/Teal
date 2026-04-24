package compose

// ServiceInfo summarises one compose service for the UI's Routes
// picker. Only carries fields the UI needs — not a full compose
// service representation.
type ServiceInfo struct {
	Name         string `json:"name"`
	Image        string `json:"image,omitempty"`
	HasBuild     bool   `json:"hasBuild"`
	ExposedPorts []int  `json:"exposedPorts,omitempty"` // from compose's `ports:` and `expose:` blocks
}

// ListServices parses YAML and returns the list of declared services.
// Used by the API to populate the Routes UI without re-parsing in
// every handler.
func ListServices(yaml string) ([]ServiceInfo, error) {
	doc, err := Parse(yaml)
	if err != nil {
		return nil, err
	}
	services := doc.services()
	out := make([]ServiceInfo, 0, len(services))
	for name, svc := range services {
		info := ServiceInfo{Name: name}
		if img, ok := svc["image"].(string); ok {
			info.Image = img
		}
		if _, ok := svc["build"]; ok {
			info.HasBuild = true
		}
		info.ExposedPorts = collectPorts(svc)
		out = append(out, info)
	}
	return out, nil
}

// collectPorts gathers container-side ports from `ports:` and
// `expose:` blocks. Best-effort — a compose with neither yields nil
// (the engine still probes at deploy time).
func collectPorts(svc map[string]any) []int {
	var out []int
	seen := map[int]struct{}{}
	add := func(p int) {
		if p <= 0 {
			return
		}
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if pr, ok := svc["ports"].([]any); ok {
		for _, raw := range pr {
			add(extractContainerPort(raw))
		}
	}
	if ex, ok := svc["expose"].([]any); ok {
		for _, raw := range ex {
			switch v := raw.(type) {
			case int:
				add(v)
			case string:
				add(parsePort(v))
			}
		}
	}
	return out
}

// extractContainerPort handles compose's port-shape variants:
//   - "3000"             → 3000
//   - "8080:3000"        → 3000  (host:container; we want container)
//   - "127.0.0.1:80:3000" → 3000
//   - 3000               → 3000  (yaml int)
//   - {target: 3000, ...} → 3000  (long form)
func extractContainerPort(raw any) int {
	switch v := raw.(type) {
	case int:
		return v
	case string:
		// Container port is the LAST colon-separated segment.
		last := v
		for i := len(v) - 1; i >= 0; i-- {
			if v[i] == ':' {
				last = v[i+1:]
				break
			}
		}
		// Strip "/tcp" or "/udp" suffix if present.
		if i := indexByte(last, '/'); i >= 0 {
			last = last[:i]
		}
		return parsePort(last)
	case map[string]any:
		if t, ok := v["target"]; ok {
			switch tv := t.(type) {
			case int:
				return tv
			case string:
				return parsePort(tv)
			}
		}
	}
	return 0
}

func parsePort(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
		if n > 65535 {
			return 0
		}
	}
	return n
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
