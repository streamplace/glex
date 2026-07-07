package glexrt

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

// ErrUnrecognizedType is returned when a $type string is not found in the
// registry during decode.
var ErrUnrecognizedType = fmt.Errorf("unrecognized lexicon type")

var (
	typeRegistry = make(map[string]reflect.Type)
	registryMu   sync.RWMutex
)

// RegisterType registers a lexicon type ID (the $type string) to a concrete Go
// type. The val argument must be a pointer to a zero value of the type (e.g.,
// &Foo{}); RegisterType records the element type. Generated code calls this
// from init() functions. Panics on duplicate registration of the same ID.
func RegisterType(id string, val any) {
	t := reflect.TypeOf(val)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if existing, ok := typeRegistry[id]; ok && existing != t {
		panic(fmt.Sprintf("glexrt: duplicate type registration for %q (existing %s, new %s)", id, existing, t))
	}
	typeRegistry[id] = t
}

// NewFromType allocates a new zero-value pointer for the type registered under
// id, or returns ErrUnrecognizedType if the id is unknown.
func NewFromType(id string) (any, error) {
	registryMu.RLock()
	t, ok := typeRegistry[id]
	registryMu.RUnlock()
	if !ok {
		return nil, xerrors.Errorf("%w: %s", ErrUnrecognizedType, id)
	}
	return reflect.New(t).Interface(), nil
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
