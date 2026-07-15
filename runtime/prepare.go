package glex

import (
	"reflect"
	"strings"

	"github.com/hyphacoop/go-dasl/drisl"
)

// drislMarshalerType matches types that marshal themselves through go-dasl
// (drisl.Marshaler). prepareForMarshal must not descend into these: they own
// their encoding, and the generated ones (unions) re-enter the runtime's
// marshal helpers, which prepare their contents in turn.
var drislMarshalerType = reflect.TypeOf((*drisl.Marshaler)(nil)).Elem()

// prepareForMarshal returns v with every nil slice/map held by a required
// (non-omitempty) struct field replaced by an empty one, copying only the
// containers it changes. drisl encodes nil slices and maps as CBOR null, but
// the atproto data model requires a required array field to be an array — and
// cbor-gen both emits an empty array for a nil slice and hard-errors
// ("expected cbor array") when it reads null. Without this, a record or event
// with an unset required array is unreadable by every cbor-gen consumer.
//
// Optional (omitempty) fields are untouched: a nil container there is omitted
// from the output entirely, which is already correct.
func prepareForMarshal(v any) any {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return v
	}
	fixed, changed := fixNilContainers(rv)
	if !changed {
		return v
	}
	return fixed.Interface()
}

func fixNilContainers(v reflect.Value) (reflect.Value, bool) {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return v, false
		}
		if selfMarshaling(v.Type()) {
			return v, false
		}
		elem, changed := fixNilContainers(v.Elem())
		if !changed {
			return v, false
		}
		np := reflect.New(v.Type().Elem())
		np.Elem().Set(elem)
		return np, true

	case reflect.Struct:
		if selfMarshaling(v.Type()) {
			return v, false
		}
		t := v.Type()
		var cp reflect.Value
		changed := false
		for i := 0; i < t.NumField(); i++ {
			ft := t.Field(i)
			if ft.PkgPath != "" {
				continue // unexported
			}
			name, opts := parseJSONTag(ft)
			if name == "-" {
				continue // not encoded
			}
			fv := v.Field(i)
			var nfv reflect.Value
			var ch bool
			if (fv.Kind() == reflect.Slice || fv.Kind() == reflect.Map) && fv.IsNil() {
				if !opts.omitempty {
					if fv.Kind() == reflect.Slice {
						nfv = reflect.MakeSlice(ft.Type, 0, 0)
					} else {
						nfv = reflect.MakeMap(ft.Type)
					}
					ch = true
				}
			} else {
				nfv, ch = fixNilContainers(fv)
			}
			if ch {
				if !changed {
					cp = reflect.New(t).Elem()
					cp.Set(v)
					changed = true
				}
				cp.Field(i).Set(nfv)
			}
		}
		if changed {
			return cp, true
		}
		return v, false

	case reflect.Slice, reflect.Array:
		var cp reflect.Value
		changed := false
		for i := 0; i < v.Len(); i++ {
			ev, ch := fixNilContainers(v.Index(i))
			if ch {
				if !changed {
					if v.Kind() == reflect.Slice {
						cp = reflect.MakeSlice(v.Type(), v.Len(), v.Len())
					} else {
						cp = reflect.New(v.Type()).Elem()
					}
					reflect.Copy(cp, v)
					changed = true
				}
				cp.Index(i).Set(ev)
			}
		}
		if changed {
			return cp, true
		}
		return v, false

	case reflect.Map:
		var cp reflect.Value
		changed := false
		iter := v.MapRange()
		for iter.Next() {
			ev, ch := fixNilContainers(iter.Value())
			if ch {
				if !changed {
					cp = reflect.MakeMapWithSize(v.Type(), v.Len())
					inner := v.MapRange()
					for inner.Next() {
						cp.SetMapIndex(inner.Key(), inner.Value())
					}
					changed = true
				}
				cp.SetMapIndex(iter.Key(), ev)
			}
		}
		if changed {
			return cp, true
		}
		return v, false
	}
	return v, false
}

// selfMarshaling reports whether t (or *t) implements drisl.Marshaler and so
// owns its own encoding.
func selfMarshaling(t reflect.Type) bool {
	return t.Implements(drislMarshalerType) || reflect.PtrTo(t).Implements(drislMarshalerType)
}

type jsonTagOpts struct {
	omitempty bool
}

// parseJSONTag returns the encoded field name and options from a `json` (or
// `cbor`, which drisl prefers when present) struct tag, following the same
// rules drisl uses for field encoding.
func parseJSONTag(ft reflect.StructField) (string, jsonTagOpts) {
	tag, ok := ft.Tag.Lookup("cbor")
	if !ok {
		tag = ft.Tag.Get("json")
	}
	if tag == "" {
		return ft.Name, jsonTagOpts{}
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = ft.Name
	}
	opts := jsonTagOpts{}
	for _, p := range parts[1:] {
		if p == "omitempty" {
			opts.omitempty = true
		}
	}
	return name, opts
}
