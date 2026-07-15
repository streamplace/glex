package glex

import (
	"fmt"
	"reflect"
	"sync"

	xerrors "golang.org/x/xerrors"
)

// CBOR is the interface implemented by generated record/object types. It
// combines marshal and unmarshal so that decoded values can be round-tripped
// through indigo's repo/carstore/MST plumbing (which expects
// cbg.CBORMarshaler/cbg.CBORUnmarshaler). Generated types satisfy this via the
// thin adapter methods emitted by writeCBORAdapter.
type CBOR interface {
	MarshalCBOR(w interface{ Write([]byte) (int, error) }) error
	UnmarshalCBOR(r interface{ Read([]byte) (int, error) }) error
}

// Record is the sealed interface implemented by every glex-generated lexicon
// type. The RecordTypeID method is emitted with a pointer receiver, which
// means only *T (never a bare T) satisfies this interface. That is
// deliberate: decode functions return Record instead of any, so a type
// assertion like rec.(placestream.Livestream) — a value type, which can
// never come out of a decoder — is a compile-time "impossible type
// assertion" error instead of a silently-false ok at runtime.
//
// Values that aren't lexicon records (e.g. a DID document destined for a
// field of lexicon type `unknown`) don't implement Record; wrap them with
// Unknown (or RawJSON) instead.
type Record interface {
	// RecordTypeID returns the lexicon $type string for this record type,
	// e.g. "place.stream.livestream" or "app.bsky.feed.defs#postView".
	RecordTypeID() string
}

// ErrUnrecognizedType is returned when a $type string is not found in the
// registry during decode.
var ErrUnrecognizedType = fmt.Errorf("unrecognized lexicon type")

// ErrWrongType is returned by the typed decode helpers (CborDecodeAs,
// JsonDecodeAs) when the record decoded successfully but is not the requested
// type.
var ErrWrongType = fmt.Errorf("record is not the requested type")

var (
	typeRegistry = make(map[string]reflect.Type)
	registryMu   sync.RWMutex
)

// RegisterType registers a lexicon type ID (the $type string) to a concrete Go
// type. The val argument must be a pointer to a zero value of the type (e.g.,
// &Foo{}); RegisterType records the element type. Generated code calls this
// from init() functions.
//
// Registering the same lexicon twice is idempotent, even when the two
// registrations are structurally identical types generated into different
// packages (e.g. two libraries that each vendored the same lexicon) — the
// first registration wins. Only registering two structurally DIFFERENT
// definitions for the same ID panics.
func RegisterType(id string, val Record) {
	t := reflect.TypeOf(val)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if existing, ok := typeRegistry[id]; ok {
		if existing == t || structurallyEqual(existing, t) {
			return
		}
		panic(fmt.Sprintf("glex: conflicting type registration for %q (existing %s, new %s)", id, existing, t))
	}
	typeRegistry[id] = t
}

// structurallyEqual reports whether two types have the same shape — same
// kinds, and for structs the same field names, tags, and (recursively) field
// types — ignoring type names and package paths. This is how two generated
// copies of the same lexicon in different packages are recognized as the same
// definition.
func structurallyEqual(a, b reflect.Type) bool {
	seen := map[[2]reflect.Type]bool{}
	var eq func(a, b reflect.Type) bool
	eq = func(a, b reflect.Type) bool {
		if a == b {
			return true
		}
		if a.Kind() != b.Kind() {
			return false
		}
		key := [2]reflect.Type{a, b}
		if seen[key] {
			// Already comparing this pair further up the stack (recursive
			// type); assume equal here — any real difference is caught at
			// the point of divergence.
			return true
		}
		seen[key] = true
		switch a.Kind() {
		case reflect.Struct:
			if a.NumField() != b.NumField() {
				return false
			}
			for i := 0; i < a.NumField(); i++ {
				fa, fb := a.Field(i), b.Field(i)
				if fa.Name != fb.Name || fa.Tag != fb.Tag || !eq(fa.Type, fb.Type) {
					return false
				}
			}
			return true
		case reflect.Ptr, reflect.Slice:
			return eq(a.Elem(), b.Elem())
		case reflect.Array:
			return a.Len() == b.Len() && eq(a.Elem(), b.Elem())
		case reflect.Map:
			return eq(a.Key(), b.Key()) && eq(a.Elem(), b.Elem())
		case reflect.Interface:
			if a.NumMethod() != b.NumMethod() {
				return false
			}
			for i := 0; i < a.NumMethod(); i++ {
				if a.Method(i).Name != b.Method(i).Name {
					return false
				}
			}
			return true
		default:
			// Same basic kind (string, int64, bool, ...).
			return true
		}
	}
	return eq(a, b)
}

// NewFromType allocates a new zero-value pointer for the type registered under
// id, or returns ErrUnrecognizedType if the id is unknown.
func NewFromType(id string) (Record, error) {
	registryMu.RLock()
	t, ok := typeRegistry[id]
	registryMu.RUnlock()
	if !ok {
		return nil, xerrors.Errorf("%w: %s", ErrUnrecognizedType, id)
	}
	// Registration takes a Record, whose methods are pointer-receiver, so
	// the freshly allocated *T is guaranteed to satisfy Record.
	return reflect.New(t).Interface().(Record), nil
}

// RegisteredTypes returns the set of registered type IDs. This is primarily
// useful for diagnostics and testing.
func RegisteredTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(typeRegistry))
	for id := range typeRegistry {
		out = append(out, id)
	}
	return out
}
