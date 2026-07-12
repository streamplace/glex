package main

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallParityWithAtprotoLex runs the same `install` command sequences
// against the first-party TypeScript `lex` CLI (@atproto/lex) and against
// `glex`, in separate sandbox directories, and asserts that the files each
// tool produces (the vendored lexicons/ tree and the lexicons.json manifest)
// are byte-for-byte identical after every step.
//
// The test needs network access (DNS `_lexicon` lookups, plc.directory, and
// PDS fetches) and a `lex` binary on $PATH (npm install -g @atproto/lex).
// It skips itself when either is unavailable or when running with -short.
func TestInstallParityWithAtprotoLex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent parity test in -short mode")
	}
	lexBin, err := exec.LookPath("lex")
	if err != nil {
		t.Skip("skipping parity test: `lex` CLI not found on $PATH (npm install -g @atproto/lex)")
	}
	glexBin := buildGlex(t)

	lexDir := t.TempDir()
	glexDir := t.TempDir()

	type step struct {
		name string
		args []string
		// mutate runs in each sandbox before the command, to set up
		// scenarios like locally modified lexicon files.
		mutate func(t *testing.T, dir string)
		// wantErr: both tools must fail (e.g. --ci with a stale manifest).
		wantErr bool
	}

	steps := []step{
		{
			name: "fresh install by NSID",
			args: []string{"install", "app.bsky.feed.like"},
		},
		{
			name: "no-op reinstall from manifest",
			args: []string{"install"},
		},
		{
			name: "ci check passes on up-to-date manifest",
			args: []string{"install", "--ci"},
		},
		{
			name: "add root by at:// URI (reuses vendored dependency)",
			args: []string{"install", "at://did:plc:6msi3pj7krzih5qxqtryxlzw/com.atproto.lexicon.schema/com.atproto.repo.strongRef"},
		},
		{
			name: "locally modified lexicon gets its CID recomputed",
			args: []string{"install"},
			mutate: func(t *testing.T, dir string) {
				editFile(t, filepath.Join(dir, "lexicons/app/bsky/feed/like.json"),
					"Record declaring a 'like'", "Record declaring a LOCALLY EDITED 'like'")
			},
		},
		{
			name: "ci check fails after local modification",
			args: []string{"install", "--ci"},
			mutate: func(t *testing.T, dir string) {
				// Change the vendored file again so its recomputed CID no
				// longer matches the manifest saved by the previous step.
				editFile(t, filepath.Join(dir, "lexicons/app/bsky/feed/like.json"),
					"LOCALLY EDITED", "DIFFERENTLY EDITED")
			},
			wantErr: true,
		},
		{
			name: "update re-fetches canonical lexicons",
			args: []string{"install", "--update"},
		},
	}

	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			if s.mutate != nil {
				s.mutate(t, lexDir)
				s.mutate(t, glexDir)
			}
			lexOut, lexErr := runInstallCLI(t, lexBin, lexDir, s.args)
			glexOut, glexErr := runInstallCLI(t, glexBin, glexDir, s.args)

			if (lexErr != nil) != (glexErr != nil) {
				t.Fatalf("exit status mismatch: lex err=%v, glex err=%v\n--- lex output ---\n%s\n--- glex output ---\n%s",
					lexErr, glexErr, lexOut, glexOut)
			}
			if s.wantErr && lexErr == nil {
				t.Fatalf("expected both tools to fail, but both succeeded")
			}
			if !s.wantErr && lexErr != nil {
				t.Fatalf("both tools failed:\n--- lex output ---\n%s\n--- glex output ---\n%s", lexOut, glexOut)
			}
			compareTrees(t, lexDir, glexDir)
		})
	}

	// Separate sandboxes: --no-save must not write a manifest, and --ci on a
	// fresh directory must fail (while still vendoring the lexicon files).
	freshSteps := []step{
		{
			name: "no-save skips manifest",
			args: []string{"install", "--no-save", "com.atproto.repo.strongRef"},
		},
		{
			name:    "ci fails without a manifest",
			args:    []string{"install", "--ci", "com.atproto.repo.strongRef"},
			wantErr: true,
		},
	}
	for _, s := range freshSteps {
		t.Run(s.name, func(t *testing.T) {
			lexDir, glexDir := t.TempDir(), t.TempDir()
			lexOut, lexErr := runInstallCLI(t, lexBin, lexDir, s.args)
			glexOut, glexErr := runInstallCLI(t, glexBin, glexDir, s.args)
			if (lexErr != nil) != (glexErr != nil) {
				t.Fatalf("exit status mismatch: lex err=%v, glex err=%v\n--- lex output ---\n%s\n--- glex output ---\n%s",
					lexErr, glexErr, lexOut, glexOut)
			}
			if s.wantErr && lexErr == nil {
				t.Fatalf("expected both tools to fail, but both succeeded")
			}
			if !s.wantErr && lexErr != nil {
				t.Fatalf("both tools failed:\n--- lex output ---\n%s\n--- glex output ---\n%s", lexOut, glexOut)
			}
			compareTrees(t, lexDir, glexDir)
		})
	}
}

// buildGlex compiles the glex CLI into a temp dir and returns the binary path.
func buildGlex(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "glex")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building glex: %v\n%s", err, out)
	}
	return bin
}

// runInstallCLI runs a CLI binary with the given args inside dir.
func runInstallCLI(t *testing.T, bin, dir string, args []string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// compareTrees asserts that two directory trees contain identical relative
// paths with identical file contents.
func compareTrees(t *testing.T, wantDir, gotDir string) {
	t.Helper()
	want := readTree(t, wantDir)
	got := readTree(t, gotDir)

	for path, wantBytes := range want {
		gotBytes, ok := got[path]
		if !ok {
			t.Errorf("missing file in glex output: %s", path)
			continue
		}
		if !bytes.Equal(wantBytes, gotBytes) {
			t.Errorf("file content mismatch for %s\n--- lex ---\n%s\n--- glex ---\n%s", path, wantBytes, gotBytes)
		}
	}
	for path := range got {
		if _, ok := want[path]; !ok {
			t.Errorf("unexpected extra file in glex output: %s", path)
		}
	}
}

// readTree returns a map of slash-separated relative paths to file contents.
func readTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	tree := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tree[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func editFile(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	replaced := strings.Replace(string(data), old, new, 1)
	if replaced == string(data) {
		t.Fatalf("editFile: %q not found in %s", old, path)
	}
	if err := os.WriteFile(path, []byte(replaced), 0o644); err != nil {
		t.Fatal(err)
	}
}
