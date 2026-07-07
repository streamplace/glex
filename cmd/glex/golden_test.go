package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	lexgen "github.com/streamplace/glex/generator"
	"go/format"
)

var update = flag.Bool("update", false, "update golden files")

// TestGoldenCodegen generates Go source from the fixture lexicons and
// compares it against committed golden files in testdata/golden/. Run with
// -update to regenerate the golden files.
func TestGoldenCodegen(t *testing.T) {
	lexiconsDir := "../../testdata/lexicons"
	goldenDir := "../../testdata/golden"

	// Build catalog from fixture lexicons
	cat := lexicon.NewBaseCatalog()
	paths, err := walkJSON(lexiconsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		var sf lexicon.SchemaFile
		b := readFile(t, p)
		if err := json.Unmarshal(b, &sf); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		if err := cat.AddSchemaFile(sf); err != nil {
			t.Fatalf("catalog %s: %v", p, err)
		}
	}

	cfg := lexgen.GlexConfig("github.com/streamplace/glex/runtime")

	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			var sf lexicon.SchemaFile
			b := readFile(t, p)
			if err := json.Unmarshal(b, &sf); err != nil {
				t.Fatal(err)
			}

			flat, err := lexgen.FlattenSchemaFile(&sf)
			if err != nil {
				t.Fatal(err)
			}

			fileCfg := *cfg
			buf := new(bytes.Buffer)
			gen := lexgen.CodeGenerator{
				Config: &fileCfg,
				Lex:    flat,
				Cat:    cat,
				Out:    buf,
			}
			if err := gen.WriteLexicon(); err != nil {
				t.Fatalf("codegen %s: %v", p, err)
			}

			// Format the output
			formatted, err := format.Source(buf.Bytes())
			if err != nil {
				t.Fatalf("format %s: %v\n--- output ---\n%s", p, err, buf.String())
			}

			goldenPath := filepath.Join(goldenDir, gen.PkgName(), gen.FileName())
			if *update {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(goldenPath, formatted, 0644); err != nil {
					t.Fatal(err)
				}
				t.Logf("updated %s", goldenPath)
				return
			}

			expected, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("reading golden file %s: %v\n(run with -update to create it)", goldenPath, err)
			}
			if !bytes.Equal(expected, formatted) {
				t.Errorf("golden mismatch for %s\n--- expected ---\n%s\n--- got ---\n%s",
					goldenPath, expected, formatted)
			}
		})
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
