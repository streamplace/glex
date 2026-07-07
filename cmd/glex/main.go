// Command glex generates Go types and XRPC clients from AT Protocol Lexicon
// schemas. It is the Go equivalent of Bluesky's TypeScript @atproto/lex tool.
//
// Serialization is canonical DAG-CBOR via go-dasl (drisl), not
// whyrusleeping/cbor-gen. Generated structs use the glex runtime package for
// the data model.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/streamplace/glex/generator"
	"github.com/urfave/cli/v3"
	"golang.org/x/tools/imports"
)

func main() {
	cmd := &cli.Command{
		Name:        "glex",
		Usage:       "Generate Go types from AT Protocol Lexicon schemas",
		Description: "glex reads Lexicon JSON schemas and emits idiomatic Go types + XRPC clients that serialize as canonical DAG-CBOR via go-dasl.",
		Commands: []*cli.Command{
			{
				Name:    "build",
				Aliases: []string{"gen", "b"},
				Usage:   "Generate Go source from lexicon JSON definitions",
				Description: "Enumerates all local lexicons (JSON files) in the lexicons directory,\nand outputs Go source files for each into the output directory.",
				ArgsUsage: `[file-or-dir]*`,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "lexicons-dir",
						Value:   "lexicons/",
						Usage:   "base directory for project Lexicon files",
						Sources: cli.EnvVars("LEXICONS_DIR"),
					},
					&cli.StringFlag{
						Name:    "output-dir",
						Aliases: []string{"out"},
						Value:   "./codegen-output/",
						Usage:   "base directory for output packages",
						Sources: cli.EnvVars("OUTPUT_DIR"),
					},
					&cli.StringFlag{
						Name:  "runtime-import",
						Usage: "Go import path of the glex runtime package (default: github.com/streamplace/glex/runtime)",
						Value: "github.com/streamplace/glex/runtime",
					},
					&cli.StringFlag{
						Name:  "runtime-alias",
						Usage: "import alias for the runtime package in generated code (default: glexrt)",
						Value: "glexrt",
					},
					&cli.BoolFlag{
						Name:  "legacy-mode",
						Usage: "use the legacy (indigo lexutil-compatible) codegen profile",
					},
					&cli.BoolFlag{
						Name:  "no-imports-tidy",
						Usage: "skip cleanup of go imports in written output",
					},
				},
				Action: runBuild,
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "glex: %v\n", err)
		os.Exit(1)
	}
}

func runBuild(ctx context.Context, cmd *cli.Command) error {
	// enumerate lexicon JSON file paths
	filePaths, err := collectPaths(cmd)
	if err != nil {
		return err
	}

	// construct full catalog of local schemas
	cat, err := collectCatalog(cmd)
	if err != nil {
		return err
	}

	runtimeImport := cmd.String("runtime-import")
	if runtimeImport == "" {
		runtimeImport = "github.com/streamplace/glex/runtime"
	}

	var cfg *lexgen.GenConfig
	if cmd.Bool("legacy-mode") {
		cfg = lexgen.LegacyConfig()
	} else {
		cfg = lexgen.GlexConfig(runtimeImport)
	}
	cfg.RuntimeAlias = cmd.String("runtime-alias")

	anyFailures := false
	for _, p := range filePaths {
		if err := genFile(ctx, cmd, cat, cfg, p); err != nil {
			fmt.Printf(" 🟠 %s\n", p)
			fmt.Printf(" [failed]: %s\n", err)
			anyFailures = true
			continue
		}
		fmt.Printf(" 🟢 %s\n", p)
	}
	if anyFailures {
		return fmt.Errorf("some codegen failed")
	}
	return nil
}

func genFile(ctx context.Context, cmd *cli.Command, cat lexicon.Catalog, cfg *lexgen.GenConfig, p string) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("failed to read lexicon schema from disk (%s): %w", p, err)
	}

	var sf lexicon.SchemaFile
	err = json.Unmarshal(b, &sf)
	if err == nil {
		err = sf.FinishParse()
	}
	if err == nil {
		err = sf.CheckSchema()
	}
	if err != nil {
		return fmt.Errorf("failed to parse lexicon schema from disk (%s): %w", p, err)
	}

	flat, err := lexgen.FlattenSchemaFile(&sf)
	if err != nil {
		return fmt.Errorf("internal codegen flattening error (%s): %w", p, err)
	}

	// Use a copy of cfg so per-file mutations don't leak
	fileCfg := *cfg

	buf := new(bytes.Buffer)
	gen := lexgen.CodeGenerator{
		Config: &fileCfg,
		Lex:    flat,
		Cat:    cat,
		Out:    buf,
	}
	if err := gen.WriteLexicon(); err != nil {
		return fmt.Errorf("failed to format codegen output (%s): %w", p, err)
	}

	outPath := path.Join(cmd.String("output-dir"), gen.PkgName(), gen.FileName())
	if err := os.MkdirAll(path.Dir(outPath), 0755); err != nil {
		return err
	}

	if !cmd.Bool("no-imports-tidy") {
		fmtOpts := imports.Options{
			Comments:  true,
			TabIndent: false,
			TabWidth:  4,
		}
		formatted, err := imports.Process(outPath, buf.Bytes(), &fmtOpts)
		if err != nil {
			return fmt.Errorf("failed to format codegen output (%s): %w", p, err)
		}
		return os.WriteFile(outPath, formatted, 0644)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to format codegen output (%s): %w", p, err)
	}
	return os.WriteFile(outPath, formatted, 0644)
}

// collectPaths enumerates lexicon JSON file paths from the lexicons-dir flag
// (or from explicit args if provided). It recursively walks directories to
// find all .json files.
func collectPaths(cmd *cli.Command) ([]string, error) {
	args := cmd.Args().Slice()
	if len(args) > 0 {
		var paths []string
		for _, a := range args {
			info, err := os.Stat(a)
			if err != nil {
				return nil, err
			}
			if info.IsDir() {
				dirPaths, err := walkJSON(a)
				if err != nil {
					return nil, err
				}
				paths = append(paths, dirPaths...)
			} else {
				paths = append(paths, a)
			}
		}
		return paths, nil
	}

	dir := cmd.String("lexicons-dir")
	return walkJSON(dir)
}

// walkJSON recursively finds all .json files under dir.
func walkJSON(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".json") {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}

// collectCatalog builds a lexicon.Catalog from all local schema files.
func collectCatalog(cmd *cli.Command) (lexicon.Catalog, error) {
	paths, err := collectPaths(cmd)
	if err != nil {
		return nil, err
	}
	cat := lexicon.NewBaseCatalog()
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("failed to read lexicon (%s): %w", p, err)
		}
		var sf lexicon.SchemaFile
		if err := json.Unmarshal(b, &sf); err != nil {
			return nil, fmt.Errorf("failed to parse lexicon (%s): %w", p, err)
		}
		if err := cat.AddSchemaFile(sf); err != nil {
			return nil, fmt.Errorf("failed to add lexicon to catalog (%s): %w", p, err)
		}
	}
	return cat, nil
}

// init registers the lexicon catalog type for the schema parser.
var _ = strings.TrimSpace
