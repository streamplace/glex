package installer

import (
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// documentNsidRefs extracts every NSID referenced by a lexicon document,
// mirroring lex-installer's listDocumentNsidRefs/defRefs walk. These are the
// dependencies that install() recursively resolves.
func documentNsidRefs(doc *orderedMap) ([]syntax.NSID, error) {
	id, _ := doc.getString("id")
	defs, ok := doc.getMap("defs")
	if !ok {
		return nil, nil
	}
	var out []syntax.NSID
	for _, name := range defs.keys {
		def, ok := defs.values[name].(*orderedMap)
		if !ok {
			continue
		}
		refs, err := defRefs(def)
		if err != nil {
			return nil, fmt.Errorf("failed to extract refs from lexicon %s: %w", id, err)
		}
		for _, ref := range refs {
			nsidStr, _, _ := strings.Cut(ref, "#")
			if nsidStr == "" {
				continue // local ref like "#images"
			}
			nsid, err := syntax.ParseNSID(nsidStr)
			if err != nil {
				return nil, fmt.Errorf("failed to extract refs from lexicon %s: %w", id, err)
			}
			out = append(out, nsid)
		}
	}
	return out, nil
}

// defRefs walks one lexicon definition and collects raw ref strings, matching
// the switch in lex-installer's defRefs generator.
func defRefs(def *orderedMap) ([]string, error) {
	typ, _ := def.getString("type")
	switch typ {
	case "string":
		// knownValues entries of the form "nsid#name" are token references.
		var out []string
		if kv, ok := def.get("knownValues"); ok {
			if arr, ok := kv.([]any); ok {
				for _, item := range arr {
					val, ok := item.(string)
					if !ok {
						continue
					}
					parts := strings.Split(val, "#")
					if len(parts) == 2 && parts[1] != "" {
						if _, err := syntax.ParseNSID(parts[0]); err == nil {
							out = append(out, val)
						}
					}
				}
			}
		}
		return out, nil
	case "array":
		items, ok := def.getMap("items")
		if !ok {
			return nil, nil
		}
		return defRefs(items)
	case "params", "object":
		var out []string
		if props, ok := def.getMap("properties"); ok {
			for _, key := range props.keys {
				prop, ok := props.values[key].(*orderedMap)
				if !ok {
					continue
				}
				refs, err := defRefs(prop)
				if err != nil {
					return nil, err
				}
				out = append(out, refs...)
			}
		}
		return out, nil
	case "union":
		var out []string
		if refs, ok := def.get("refs"); ok {
			if arr, ok := refs.([]any); ok {
				for _, item := range arr {
					if s, ok := item.(string); ok {
						out = append(out, s)
					}
				}
			}
		}
		return out, nil
	case "ref":
		if ref, ok := def.getString("ref"); ok {
			return []string{ref}, nil
		}
		return nil, nil
	case "record":
		record, ok := def.getMap("record")
		if !ok {
			return nil, nil
		}
		return defRefs(record)
	case "procedure", "query", "subscription":
		var out []string
		if typ == "procedure" {
			if schema := ioSchema(def, "input"); schema != nil {
				refs, err := defRefs(schema)
				if err != nil {
					return nil, err
				}
				out = append(out, refs...)
			}
		}
		if typ == "procedure" || typ == "query" {
			if schema := ioSchema(def, "output"); schema != nil {
				refs, err := defRefs(schema)
				if err != nil {
					return nil, err
				}
				out = append(out, refs...)
			}
		}
		if params, ok := def.getMap("parameters"); ok {
			refs, err := defRefs(params)
			if err != nil {
				return nil, err
			}
			out = append(out, refs...)
		}
		if schema := ioSchema(def, "message"); schema != nil {
			refs, err := defRefs(schema)
			if err != nil {
				return nil, err
			}
			out = append(out, refs...)
		}
		return out, nil
	case "permission-set":
		var out []string
		if perms, ok := def.get("permissions"); ok {
			if arr, ok := perms.([]any); ok {
				for _, item := range arr {
					perm, ok := item.(*orderedMap)
					if !ok {
						continue
					}
					refs, err := defRefs(perm)
					if err != nil {
						return nil, err
					}
					out = append(out, refs...)
				}
			}
		}
		return out, nil
	case "permission":
		resource, _ := def.getString("resource")
		var field string
		switch resource {
		case "rpc":
			field = "lxm"
		case "repo":
			field = "collection"
		default:
			return nil, nil
		}
		var out []string
		if v, ok := def.get(field); ok {
			if arr, ok := v.([]any); ok {
				for _, item := range arr {
					if s, ok := item.(string); ok {
						out = append(out, s)
					}
				}
			}
		}
		return out, nil
	case "boolean", "cid-link", "token", "bytes", "blob", "integer", "unknown":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown lexicon def type: %s", typ)
	}
}

// ioSchema returns def[field].schema when present (procedure/query
// input/output and subscription message wrappers).
func ioSchema(def *orderedMap, field string) *orderedMap {
	wrapper, ok := def.getMap(field)
	if !ok {
		return nil
	}
	schema, ok := wrapper.getMap("schema")
	if !ok {
		return nil
	}
	return schema
}
