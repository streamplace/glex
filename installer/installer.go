// Package installer implements `glex install`, a port of @atproto/lex's
// repo-vendoring and lockfile support (lex install). It fetches lexicon
// records from the network with cryptographic proof verification, vendors
// them as JSON files under a lexicons directory, tracks resolutions in a
// lexicons.json manifest, and recursively installs referenced lexicons.
//
// Output is bitexact with @atproto/lex: lexicon files preserve the record's
// CBOR key order and manifests are normalized, both serialized in
// JSON.stringify(value, null, 2) form.
package installer

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/hyphacoop/go-dasl/drisl"
)

// Options configures Install, mirroring the `lex install` CLI flags.
type Options struct {
	// Manifest is the path to the lexicons.json manifest file.
	Manifest string
	// Lexicons is the directory lexicon JSON files are vendored into.
	Lexicons string
	// Add lists lexicons to install: NSIDs or at:// URIs.
	Add []string
	// Save writes the updated manifest after installation.
	Save bool
	// Update re-fetches all lexicons from the network instead of reusing
	// local files or manifest resolutions.
	Update bool
	// CI errors if installation changes the manifest (lockfile check).
	CI bool
	// Log receives progress messages; defaults to discarding them.
	Log func(format string, args ...any)
}

// Install fetches and installs lexicon documents, mirroring
// @atproto/lex-installer's install().
func Install(ctx context.Context, opts Options) error {
	if opts.Log == nil {
		opts.Log = func(string, ...any) {}
	}

	prev, err := readManifest(opts.Manifest)
	if err != nil {
		return err
	}

	inst := &installer{
		opts:     opts,
		resolver: newResolver(),
		indexer:  newDirIndexer(opts.Lexicons),
		docs:     map[string]*orderedMap{},
		manifest: newManifest(),
	}

	if err := inst.install(ctx, prev); err != nil {
		return err
	}

	if opts.CI && (prev == nil || !inst.manifest.equals(prev)) {
		return fmt.Errorf("Lexicons manifest is out of date")
	}

	if opts.Save {
		return writeFileJSON(opts.Manifest, inst.manifest.stringify())
	}
	return nil
}

type installer struct {
	opts     Options
	resolver *resolver
	indexer  *dirIndexer
	docs     map[string]*orderedMap // installed documents by id
	manifest *Manifest              // manifest being built
}

type rootEntry struct {
	nsid syntax.NSID
	uri  syntax.ATURI // empty: resolve from NSID
}

func (inst *installer) install(ctx context.Context, prev *Manifest) error {
	var roots []rootEntry
	seen := map[string]bool{}

	// First, process explicit additions (deduplicating identical strings).
	seenAdditions := map[string]bool{}
	for _, addition := range inst.opts.Add {
		if seenAdditions[addition] {
			continue
		}
		seenAdditions[addition] = true

		var nsid syntax.NSID
		var uri syntax.ATURI
		if strings.HasPrefix(addition, "at://") {
			u, err := syntax.ParseATURI(addition)
			if err != nil {
				return fmt.Errorf("invalid lexicon addition %q: %w", addition, err)
			}
			nsid, err = syntax.ParseNSID(u.RecordKey().String())
			if err != nil {
				return fmt.Errorf("invalid lexicon addition %q: %w", addition, err)
			}
			uri = u
		} else {
			n, err := syntax.ParseNSID(addition)
			if err != nil {
				return fmt.Errorf("invalid lexicon addition %q: %w", addition, err)
			}
			nsid = n
		}

		if seen[nsid.String()] {
			return fmt.Errorf("duplicate lexicon addition: %s (%s)", nsid, addition)
		}
		seen[nsid.String()] = true
		roots = append(roots, rootEntry{nsid: nsid, uri: uri})
		if uri != "" {
			inst.opts.Log("Adding new lexicon: %s (%s)", nsid, uri)
		} else {
			inst.opts.Log("Adding new lexicon: %s (from NSID)", nsid)
		}
	}

	// Next, restore previously existing manifest entries.
	if prev != nil {
		for _, lexicon := range prev.Lexicons {
			nsid, err := syntax.ParseNSID(lexicon)
			if err != nil {
				return fmt.Errorf("invalid lexicon %q in manifest: %w", lexicon, err)
			}
			if seen[nsid.String()] {
				continue
			}
			seen[nsid.String()] = true
			var uri syntax.ATURI
			if res, ok := prev.Resolutions[lexicon]; ok {
				uri = syntax.ATURI(res.URI)
				inst.opts.Log("Adding lexicon from manifest: %s (%s)", nsid, uri)
			} else {
				inst.opts.Log("Adding lexicon from manifest: %s (from NSID)", nsid)
			}
			roots = append(roots, rootEntry{nsid: nsid, uri: uri})
		}
	}

	// Install all root lexicons (and record them in the manifest).
	for _, root := range roots {
		inst.opts.Log("Installing lexicon: %s", root.nsid)
		var lex *resolvedLexicon
		var err error
		if root.uri != "" {
			lex, err = inst.installFromURI(ctx, root.uri)
		} else {
			lex, err = inst.installFromNSID(ctx, root.nsid)
		}
		if err != nil {
			return err
		}
		inst.manifest.Lexicons = append(inst.manifest.Lexicons, lex.id)
	}

	// Then recursively install all referenced lexicons.
	for {
		missing, err := inst.missingIDs()
		if err != nil {
			return err
		}
		if len(missing) == 0 {
			return nil
		}
		for _, nsid := range missing {
			inst.opts.Log("Resolving dependency lexicon: %s", nsid)
			var resolvedURI syntax.ATURI
			if prev != nil {
				if res, ok := prev.Resolutions[nsid.String()]; ok {
					resolvedURI = syntax.ATURI(res.URI)
				}
			}
			var err error
			if resolvedURI != "" {
				_, err = inst.installFromURI(ctx, resolvedURI)
			} else {
				_, err = inst.installFromNSID(ctx, nsid)
			}
			if err != nil {
				return err
			}
		}
	}
}

// missingIDs collects NSIDs referenced by installed documents that are not
// yet installed themselves, preserving first-seen order.
func (inst *installer) missingIDs() ([]syntax.NSID, error) {
	var missing []syntax.NSID
	seen := map[string]bool{}
	for _, id := range sortedKeys(inst.docs) {
		refs, err := documentNsidRefs(inst.docs[id])
		if err != nil {
			return nil, err
		}
		for _, nsid := range refs {
			key := nsid.String()
			if inst.docs[key] != nil || seen[key] {
				continue
			}
			seen[key] = true
			missing = append(missing, nsid)
		}
	}
	return missing, nil
}

func (inst *installer) installFromNSID(ctx context.Context, nsid syntax.NSID) (*resolvedLexicon, error) {
	uri, err := inst.resolver.resolveNSID(ctx, nsid)
	if err != nil {
		return nil, err
	}
	return inst.installFromURI(ctx, uri)
}

func (inst *installer) installFromURI(ctx context.Context, uri syntax.ATURI) (*resolvedLexicon, error) {
	var lex *resolvedLexicon

	if !inst.opts.Update {
		doc, err := inst.indexer.get(uri.RecordKey().String())
		if err != nil {
			return nil, err
		}
		if doc != nil {
			inst.opts.Log("Re-using existing lexicon %s from indexer", uri.RecordKey())
			cidStr, err := cidForDoc(doc)
			if err != nil {
				return nil, err
			}
			id, _ := doc.getString("id")
			lex = &resolvedLexicon{doc: doc, id: id, cid: cidStr}
		}
	}

	if lex == nil {
		fetched, err := inst.fetchAndStore(ctx, uri)
		if err != nil {
			return nil, err
		}
		lex = fetched
	}

	inst.docs[lex.id] = lex.doc
	inst.manifest.Resolutions[lex.id] = Resolution{URI: uri.String(), CID: lex.cid}
	return lex, nil
}

// fetchAndStore fetches a lexicon record from the network and vendors it
// into the lexicons directory, like LexInstaller.fetch.
func (inst *installer) fetchAndStore(ctx context.Context, uri syntax.ATURI) (*resolvedLexicon, error) {
	inst.opts.Log("Fetching lexicon from %s...", uri)
	lex, err := inst.resolver.fetch(ctx, uri, inst.opts.Update)
	if err != nil {
		return nil, err
	}
	segments := strings.Split(lex.id, ".")
	path := filepath.Join(append([]string{inst.opts.Lexicons}, segments...)...) + ".json"
	if err := writeFileJSON(path, stringifyJSON(lex.doc)); err != nil {
		return nil, err
	}
	return lex, nil
}

// cidForDoc computes the CID a locally stored lexicon document would have as
// an AT Protocol record (canonical DAG-CBOR, sha-256), the equivalent of
// @atproto/lex-cbor's cidForLex.
func cidForDoc(doc *orderedMap) (string, error) {
	plain, err := toPlainValue(doc)
	if err != nil {
		return "", err
	}
	c, err := drisl.CidForValue(plain)
	if err != nil {
		return "", err
	}
	return c.String(), nil
}

func sortedKeys(m map[string]*orderedMap) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Deterministic iteration; the exact order does not affect output files.
	sort.Strings(keys)
	return keys
}
