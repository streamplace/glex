package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Resolution records where a lexicon was fetched from and the CID of the
// record, forming the lockfile half of the lexicons.json manifest.
type Resolution struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

// Manifest mirrors @atproto/lex's lexicons.json manifest: the directly
// requested lexicons plus resolution info for every installed lexicon
// (including transitive dependencies).
type Manifest struct {
	Version     int                   `json:"version"`
	Lexicons    []string              `json:"lexicons"`
	Resolutions map[string]Resolution `json:"resolutions"`
}

func newManifest() *Manifest {
	return &Manifest{Version: 1, Lexicons: []string{}, Resolutions: map[string]Resolution{}}
}

// readManifest loads a manifest file. A missing file returns (nil, nil);
// any other failure is an error, matching lex-installer's install().
func readManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read lexicons manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to read lexicons manifest: %w", err)
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("failed to read lexicons manifest: unsupported version %d", m.Version)
	}
	if m.Lexicons == nil {
		m.Lexicons = []string{}
	}
	if m.Resolutions == nil {
		m.Resolutions = map[string]Resolution{}
	}
	return &m, nil
}

// normalized returns a copy with lexicons and resolution keys sorted, the
// same canonicalization lex-installer applies before saving or comparing.
func (m *Manifest) normalized() *Manifest {
	out := &Manifest{
		Version:     m.Version,
		Lexicons:    append([]string{}, m.Lexicons...),
		Resolutions: make(map[string]Resolution, len(m.Resolutions)),
	}
	sort.Strings(out.Lexicons)
	for k, v := range m.Resolutions {
		out.Resolutions[k] = v
	}
	return out
}

// equals compares two manifests after normalization.
func (m *Manifest) equals(other *Manifest) bool {
	a, b := m.normalized(), other.normalized()
	if a.Version != b.Version || len(a.Lexicons) != len(b.Lexicons) || len(a.Resolutions) != len(b.Resolutions) {
		return false
	}
	for i := range a.Lexicons {
		if a.Lexicons[i] != b.Lexicons[i] {
			return false
		}
	}
	for k, v := range a.Resolutions {
		if b.Resolutions[k] != v {
			return false
		}
	}
	return true
}

// stringify renders the normalized manifest exactly as @atproto/lex writes it:
// JSON.stringify(manifest, null, 2) with sorted lexicons and resolution keys.
func (m *Manifest) stringify() string {
	n := m.normalized()
	root := newOrderedMap()
	root.set("version", int64(n.Version))
	lexicons := make([]any, len(n.Lexicons))
	for i, l := range n.Lexicons {
		lexicons[i] = l
	}
	root.set("lexicons", lexicons)
	resolutions := newOrderedMap()
	keys := make([]string, 0, len(n.Resolutions))
	for k := range n.Resolutions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		entry := newOrderedMap()
		entry.set("uri", n.Resolutions[k].URI)
		entry.set("cid", n.Resolutions[k].CID)
		resolutions.set(k, entry)
	}
	root.set("resolutions", resolutions)
	return stringifyJSON(root)
}

// writeFileJSON writes JSON content the way lex-installer's writeJsonFile
// does: parent directories created, file mode 0644, no trailing newline.
func writeFileJSON(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
