package compose

import (
	"regexp"
	"sort"
	"strings"
)

// EnvVarRef is one environment variable the compose project references.
// The UI surfaces these as "vars this app needs"; the engine treats
// them as the keys to inject as build args + write to the runtime
// env-file.
type EnvVarRef struct {
	// Name is the variable name, without the ${} wrapping.
	Name string `json:"name"`

	// HasDefault is true when at least one reference uses
	// ${VAR:-default} syntax. The engine treats these as optional —
	// missing values fall back to the default at compose render
	// time.
	HasDefault bool `json:"hasDefault"`

	// DefaultValue is the literal default from the FIRST reference
	// that supplied one. Conflicting defaults across references are
	// not flagged; first wins.
	DefaultValue string `json:"defaultValue,omitempty"`

	// Sources lists the YAML paths where the var was referenced,
	// e.g. "services.app.environment.APP_URL", "services.app.image".
	// The UI renders this so users can see WHY a var is needed.
	Sources []string `json:"sources"`
}

// interpolation patterns compose understands. We honour the full set
// (with/without colon for default, error, or unset-marker forms) so
// nothing slips past discovery — even a $VAR without braces.
var (
	bracedRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::?[-?+]([^}]*))?\}`)
	plainRef  = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)
)

// DiscoverEnvVars walks the parsed compose YAML and returns every
// distinct env var the project references. Two flavours of reference
// are picked up:
//
//  1. ${VAR} / ${VAR:-default} interpolations anywhere in the YAML
//     (image tags, environment values, ports, command args, etc.).
//  2. Service `environment:` block KEYS — even when the user didn't
//     write `${VAR}`, declaring `APP_URL: ` (empty value) or
//     `APP_URL: ${APP_URL}` both signal "this needs to be passed to
//     the container."
//
// Returned slice is sorted by Name for stable UI rendering.
func DiscoverEnvVars(yaml string) ([]EnvVarRef, error) {
	doc, err := Parse(yaml)
	if err != nil {
		return nil, err
	}
	acc := newEnvAccumulator()
	walk(doc.root, "", acc)
	return acc.list(), nil
}

// envAccumulator collects refs by name, merging Sources and picking
// the first observed default.
type envAccumulator struct {
	byName map[string]*EnvVarRef
}

func newEnvAccumulator() *envAccumulator {
	return &envAccumulator{byName: map[string]*EnvVarRef{}}
}

func (a *envAccumulator) note(name, source string, hasDefault bool, def string) {
	if name == "" {
		return
	}
	ref, ok := a.byName[name]
	if !ok {
		ref = &EnvVarRef{Name: name}
		a.byName[name] = ref
	}
	if source != "" {
		// dedupe sources — the same var referenced twice in one
		// place shouldn't show twice.
		for _, s := range ref.Sources {
			if s == source {
				goto skipSource
			}
		}
		ref.Sources = append(ref.Sources, source)
	skipSource:
	}
	if hasDefault && !ref.HasDefault {
		ref.HasDefault = true
		ref.DefaultValue = def
	}
}

func (a *envAccumulator) list() []EnvVarRef {
	out := make([]EnvVarRef, 0, len(a.byName))
	for _, ref := range a.byName {
		sort.Strings(ref.Sources)
		out = append(out, *ref)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// walk recursively visits the YAML tree. Strings get scanned for
// interpolations; maps get path-tracked for the Sources field.
// Special-case: a service's `environment:` block treats KEYS as
// references too.
func walk(node any, path string, acc *envAccumulator) {
	switch v := node.(type) {
	case string:
		scanInterpolations(v, path, acc)
	case map[string]any:
		// Detect environment blocks anywhere. The path tail
		// "environment" identifies them regardless of whether
		// they're under services.X or x-anchors.Y.
		isEnv := strings.HasSuffix(path, ".environment")
		for k, child := range v {
			childPath := joinPath(path, k)
			if isEnv {
				// Map form: { KEY: value }. The KEY itself is a
				// var reference; the value may also reference other
				// vars (${OTHER_VAR}).
				acc.note(k, childPath, false, "")
			}
			walk(child, childPath, acc)
		}
	case []any:
		// Detect environment blocks in list form too:
		//   environment:
		//     - KEY=value
		//     - KEY            (uses host env)
		//     - KEY=${OTHER}
		isEnv := strings.HasSuffix(path, ".environment")
		for i, child := range v {
			elemPath := joinPath(path, indexLabel(i))
			if isEnv {
				if s, ok := child.(string); ok {
					name, val, hasEq := strings.Cut(s, "=")
					name = strings.TrimSpace(name)
					acc.note(name, joinPath(path, name), false, "")
					if hasEq {
						scanInterpolations(val, joinPath(path, name), acc)
					}
					continue
				}
			}
			walk(child, elemPath, acc)
		}
	}
}

func scanInterpolations(s, source string, acc *envAccumulator) {
	for _, m := range bracedRef.FindAllStringSubmatch(s, -1) {
		name := m[1]
		def := ""
		hasDef := false
		if len(m) >= 3 && m[2] != "" {
			// Only treat ${VAR:-default} / ${VAR-default} as
			// providing a default. The :? / ? forms are "fail if
			// unset" — they don't supply a value, just an error
			// message.
			if strings.Contains(s, "${"+name+":-") || strings.Contains(s, "${"+name+"-") {
				hasDef = true
				def = m[2]
			}
		}
		acc.note(name, source, hasDef, def)
	}
	// Plain $VAR (no braces). Compose accepts this too. Skip when
	// it overlaps with a braced ref we already captured.
	stripped := bracedRef.ReplaceAllString(s, "")
	for _, m := range plainRef.FindAllStringSubmatch(stripped, -1) {
		acc.note(m[1], source, false, "")
	}
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func indexLabel(i int) string {
	// keep paths human-readable for the UI: "ports.0" rather than
	// "ports[0]". Compose users grok dot paths.
	return itoa(i)
}

func itoa(i int) string {
	// avoid pulling in strconv just for this
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
