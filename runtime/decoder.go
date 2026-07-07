package glexrt

import (
	"encoding/json"
	"io"
	"reflect"

	"github.com/hyphacoop/go-dasl/drisl"
	xerrors "golang.org/x/xerrors"
)

// CborDecodeValue decodes a DAG-CBOR record by dispatching on its $type field.
// It extracts $type from the CBOR bytes, looks up the concrete Go type in the
// registry, allocates a new instance, and unmarshals the full bytes into it.
// Returns ErrUnrecognizedType if the $type is not registered.
//
// This is the firehose workhorse: a consumer reads a record body from a
// subscription/repo stream and calls CborDecodeValue to get back a typed value.
func CborDecodeValue(b []byte) (any, error) {
	typ, err := typeExtractCBOR(b)
	if err != nil {
		return nil, xerrors.Errorf("extracting $type from CBOR: %w", err)
	}
	if typ == "" {
		return nil, xerrors.Errorf("%w: empty $type", ErrUnrecognizedType)
	}
	val, err := NewFromType(typ)
	if err != nil {
		return nil, err
	}
	if err := drisl.Unmarshal(b, val); err != nil {
		return nil, xerrors.Errorf("decoding %s: %w", typ, err)
	}
	return val, nil
}

// JsonDecodeValue decodes a JSON record by dispatching on its $type field.
// It extracts $type from the JSON bytes, looks up the concrete Go type in the
// registry, allocates a new instance, and unmarshals the full bytes into it.
// Returns ErrUnrecognizedType if the $type is not registered.
func JsonDecodeValue(b []byte) (any, error) {
	typ, err := typeExtractJSON(b)
	if err != nil {
		return nil, xerrors.Errorf("extracting $type from JSON: %w", err)
	}
	if typ == "" {
		return nil, xerrors.Errorf("%w: empty $type", ErrUnrecognizedType)
	}
	val, err := NewFromType(typ)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, val); err != nil {
		return nil, xerrors.Errorf("decoding %s: %w", typ, err)
	}
	return val, nil
}

// CborDecodeReader is like CborDecodeValue but reads from an io.Reader. It
// reads all bytes first, then dispatches.
func CborDecodeReader(r io.Reader) (any, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return CborDecodeValue(b)
}

// LexiconTypeDecoder is the open "unknown record" wrapper used in view types
// (e.g., a feed view's Record field, which could be any record type). It
// holds the decoded value and marshals it back with its $type.
//
// On unmarshal (JSON or CBOR), it dispatches via the type registry to decode
// into the concrete type. If the $type is unrecognized, it stores the raw
// bytes so the value can still be round-tripped.
type LexiconTypeDecoder struct {
	Val any
}

// UnmarshalJSON implements json.Unmarshaler.
func (ltd *LexiconTypeDecoder) UnmarshalJSON(b []byte) error {
	val, err := JsonDecodeValue(b)
	if err != nil {
		if xerrors.Is(err, ErrUnrecognizedType) {
			// Store raw bytes so the value can be round-tripped even if the
			// type is not registered.
			ltd.Val = json.RawMessage(b)
			return nil
		}
		return err
	}
	ltd.Val = val
	return nil
}

// MarshalJSON implements json.Marshaler.
func (ltd *LexiconTypeDecoder) MarshalJSON() ([]byte, error) {
	if ltd == nil || ltd.Val == nil {
		return []byte("null"), nil
	}
	// If the value was stored as raw bytes (unrecognized type), return them
	// as-is.
	if raw, ok := ltd.Val.(json.RawMessage); ok {
		return raw, nil
	}
	// Ensure the $type field is set on the record before marshaling, by
	// reflecting on the LexiconTypeID field if present.
	setTypeIDFromTag(ltd.Val)
	return json.Marshal(ltd.Val)
}

// UnmarshalCBOR implements go-dasl's Unmarshaler.
func (ltd *LexiconTypeDecoder) UnmarshalCBOR(b []byte) error {
	val, err := CborDecodeValue(b)
	if err != nil {
		if xerrors.Is(err, ErrUnrecognizedType) {
			// Store raw bytes for round-tripping.
			ltd.Val = append([]byte(nil), b...)
			return nil
		}
		return err
	}
	ltd.Val = val
	return nil
}

// MarshalCBOR implements go-dasl's Marshaler.
func (ltd *LexiconTypeDecoder) MarshalCBOR() ([]byte, error) {
	if ltd == nil || ltd.Val == nil {
		return drisl.Marshal(nil)
	}
	// If the value was stored as raw bytes, return them as-is.
	if raw, ok := ltd.Val.([]byte); ok {
		return raw, nil
	}
	setTypeIDFromTag(ltd.Val)
	return drisl.Marshal(ltd.Val)
}

// setTypeIDFromTag looks for a LexiconTypeID field on the value's struct (via
// reflection) and, if the struct has a cborgen tag with a const= value, sets
// the field to that constant. This ensures $type is always present in output
// even if the caller didn't set it.
func setTypeIDFromTag(val any) {
	v := reflect.ValueOf(val)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("cborgen")
		if tag == "" {
			continue
		}
		// Look for const= in the cborgen tag (format: "name,const=value" or
		// "name,omitempty,const=value").
		constPrefix := "const="
		idx := indexOf(tag, constPrefix)
		if idx < 0 {
			continue
		}
		// Verify this field is the $type field.
		jsonTag := f.Tag.Get("json")
		if jsonTag != "$type" {
			continue
		}
		constStart := idx + len(constPrefix)
		constVal := extractToken(tag[constStart:])
		if constVal == "" {
			continue
		}
		if fv := v.Field(i); fv.CanSet() && fv.Kind() == reflect.String {
			fv.SetString(constVal)
		}
	}
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func extractToken(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ',' || c == ' ' || c == '\t' {
			break
		}
		out = append(out, c)
	}
	return string(out)
}
