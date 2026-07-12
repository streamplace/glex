package installer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// dirIndexer indexes lexicon documents already present in the lexicons
// directory, keyed by their "id" field (not their path), like lex-builder's
// LexiconDirectoryIndexer. It lets install() reuse local files instead of
// re-fetching from the network.
type dirIndexer struct {
	dir     string
	scanned bool
	docs    map[string]*orderedMap
}

func newDirIndexer(dir string) *dirIndexer {
	return &dirIndexer{dir: dir, docs: map[string]*orderedMap{}}
}

// get returns the local lexicon document with the given id, or
// (nil, nil) when no such document exists locally.
func (x *dirIndexer) get(id string) (*orderedMap, error) {
	if !x.scanned {
		if err := x.scan(); err != nil {
			return nil, err
		}
		x.scanned = true
	}
	return x.docs[id], nil
}

func (x *dirIndexer) scan() error {
	err := filepath.WalkDir(x.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		parsed, err := parseJSON(data)
		if err != nil {
			return fmt.Errorf("error parsing lexicon document %s: %w", path, err)
		}
		doc, ok := parsed.(*orderedMap)
		if !ok {
			return fmt.Errorf("error parsing lexicon document %s: not an object", path)
		}
		if err := validateLexiconDoc(doc); err != nil {
			return fmt.Errorf("error parsing lexicon document %s: %w", path, err)
		}
		id, _ := doc.getString("id")
		x.docs[id] = doc
		return nil
	})
	if err != nil && os.IsNotExist(err) {
		// A missing lexicons directory is just an empty index.
		return nil
	}
	return err
}
