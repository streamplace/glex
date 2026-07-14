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
// from init() functions. Panics on duplicate registration of the same ID.
func RegisterType(id string, val Record) {
	t := reflect.TypeOf(val)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if existing, ok := typeRegistry[id]; ok && existing != t {
		panic(fmt.Sprintf("glex: duplicate type registration for %q (existing %s, new %s)", id, existing, t))
	}
	typeRegistry[id] = t
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
