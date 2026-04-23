package compose

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Render serialises a Document back to YAML. The output uses 2-space
// indentation (compose convention). Maps are sorted alphabetically by
// yaml.v3, which keeps diffs readable.
func Render(doc *Document) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc.root); err != nil {
		return "", fmt.Errorf("compose: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("compose: encode close: %w", err)
	}
	return buf.String(), nil
}

// WriteFile writes a transformed compose YAML into dir/compose.yml,
// creating the directory if necessary. Returns the full path.
func WriteFile(dir, content string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("compose: ensure dir: %w", err)
	}
	path := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("compose: write: %w", err)
	}
	return path, nil
}
