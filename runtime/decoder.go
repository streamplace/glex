package glex

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
// The result is always a pointer to a generated type (e.g. *Livestream); when
// the expected type is known, prefer CborDecodeAs.
func CborDecodeValue(b []byte) (Record, error) {
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
func JsonDecodeValue(b []byte) (Record, error) {
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
func CborDecodeReader(r io.Reader) (Record, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return CborDecodeValue(b)
}

// DecodeCBOR decodes a DAG-CBOR record into the given generated type,
// verifying the wire bytes' $type against the target's RecordTypeID first:
//
//	var video placestream.Video
//	if err := glex.DecodeCBOR(b, &video); err != nil { ... }
//
// This is the preferred decode call when the caller knows what the record
// must be. Unlike a bare unmarshal, bytes of any other type are a hard error
// (wrapping ErrWrongType) instead of a silently zero-filled struct; and
// because only pointers to generated types satisfy Record, passing a bare
// value (or a non-lexicon type) is a compile error.
func DecodeCBOR(b []byte, rec Record) error {
	if rec == nil {
		return xerrors.Errorf("DecodeCBOR into nil Record")
	}
	typ, err := typeExtractCBOR(b)
	if err != nil {
		return xerrors.Errorf("extracting $type from CBOR: %w", err)
	}
	if want := rec.RecordTypeID(); typ != want {
		return xerrors.Errorf("%w: bytes contain %q, decoding into %T (%s)", ErrWrongType, typ, rec, want)
	}
	return drisl.Unmarshal(b, rec)
}

// DecodeJSON is DecodeCBOR for JSON records.
func DecodeJSON(b []byte, rec Record) error {
	if rec == nil {
		return xerrors.Errorf("DecodeJSON into nil Record")
	}
	typ, err := typeExtractJSON(b)
	if err != nil {
		return xerrors.Errorf("extracting $type from JSON: %w", err)
	}
	if want := rec.RecordTypeID(); typ != want {
		return xerrors.Errorf("%w: bytes contain %q, decoding into %T (%s)", ErrWrongType, typ, rec, want)
	}
	return json.Unmarshal(b, rec)
}

// CborDecodeAs is expression-shaped sugar over DecodeCBOR, for call sites
// that want the decoded record as a fresh pointer:
//
//	ls, err := glex.CborDecodeAs[placestream.Livestream](b)
func CborDecodeAs[T any, PT interface {
	*T
	Record
}](b []byte) (*T, error) {
	t := new(T)
	if err := DecodeCBOR(b, PT(t)); err != nil {
		return nil, err
	}
	return t, nil
}

// JsonDecodeAs is CborDecodeAs for JSON records.
func JsonDecodeAs[T any, PT interface {
	*T
	Record
}](b []byte) (*T, error) {
	t := new(T)
	if err := DecodeJSON(b, PT(t)); err != nil {
		return nil, err
	}
	return t, nil
}

// RecordAs asserts a decoded Record (e.g. a LexiconTypeDecoder.Val) to the
// given generated type, returning an error (wrapping ErrWrongType) on
// mismatch or nil input.
func RecordAs[T any, PT interface {
	*T
	Record
}](rec Record) (*T, error) {
	if rec == nil {
		return nil, xerrors.Errorf("%w: record is nil, expected %T", ErrWrongType, PT(nil))
	}
	typed, ok := rec.(PT)
	if !ok {
		return nil, xerrors.Errorf("%w: have %s (%T), expected %T", ErrWrongType, rec.RecordTypeID(), rec, PT(nil))
	}
	return (*T)(typed), nil
}

// RawRecord holds the bytes of a record whose $type is not in the registry,
// so unknown records can still round-trip through LexiconTypeDecoder. It
// implements Record.
type RawRecord struct {
	// Type is the $type extracted from the bytes (may be empty).
	Type string
	// Encoding is "json" or "cbor", matching the wire format of Bytes.
	Encoding string
	Bytes    []byte
}

func (r *RawRecord) RecordTypeID() string { return r.Type }

// Unknown wraps an arbitrary JSON-marshalable value for a lexicon field of
// type `unknown` (generated as *LexiconTypeDecoder) whose content is not a
// lexicon record — e.g. a DID document:
//
//	didDoc, err := glex.Unknown(ident.DIDDocument())
//	out.DidDoc = didDoc
func Unknown(v any) (*LexiconTypeDecoder, error) {
	raw, err := RawJSON(v)
	if err != nil {
		return nil, err
	}
	return &LexiconTypeDecoder{Val: raw}, nil
}

// RawJSON wraps an arbitrary JSON-marshalable value as a *RawRecord, for
// carrying non-lexicon payloads (e.g. a DID document) in a
// LexiconTypeDecoder field. The wrapped value round-trips through JSON
// marshaling as-is. Unknown is the one-call version for the common case.
func RawJSON(v any) (*RawRecord, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	typ, _ := typeExtractJSON(b)
	return &RawRecord{Type: typ, Encoding: "json", Bytes: b}, nil
}

// LexiconTypeDecoder is the open "unknown record" wrapper used in view types
// (e.g., a feed view's Record field, which could be any record type). It
// holds the decoded value and marshals it back with its $type.
//
// On unmarshal (JSON or CBOR), it dispatches via the type registry to decode
// into the concrete type. If the $type is unrecognized, it stores a
// *RawRecord so the value can still be round-tripped.
type LexiconTypeDecoder struct {
	Val Record
}

// UnmarshalJSON implements json.Unmarshaler.
func (ltd *LexiconTypeDecoder) UnmarshalJSON(b []byte) error {
	val, err := JsonDecodeValue(b)
	if err != nil {
		if xerrors.Is(err, ErrUnrecognizedType) {
			// Store raw bytes so the value can be round-tripped even if the
			// type is not registered.
			typ, _ := typeExtractJSON(b)
			ltd.Val = &RawRecord{Type: typ, Encoding: "json", Bytes: append([]byte(nil), b...)}
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
	// If the value was stored raw (unrecognized type), return it as-is.
	if raw, ok := ltd.Val.(*RawRecord); ok {
		if raw.Encoding != "json" {
			return nil, xerrors.Errorf("cannot marshal raw %s record as JSON", raw.Encoding)
		}
		return raw.Bytes, nil
	}
	// Ensure the $type field is set in the output, without mutating Val.
	return json.Marshal(stampedForMarshal(ltd.Val))
}

// UnmarshalCBOR implements go-dasl's Unmarshaler.
func (ltd *LexiconTypeDecoder) UnmarshalCBOR(b []byte) error {
	val, err := CborDecodeValue(b)
	if err != nil {
		if xerrors.Is(err, ErrUnrecognizedType) {
			// Store raw bytes for round-tripping.
			typ, _ := typeExtractCBOR(b)
			ltd.Val = &RawRecord{Type: typ, Encoding: "cbor", Bytes: append([]byte(nil), b...)}
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
	// If the value was stored raw, return it as-is.
	if raw, ok := ltd.Val.(*RawRecord); ok {
		if raw.Encoding != "cbor" {
			return nil, xerrors.Errorf("cannot marshal raw %s record as CBOR", raw.Encoding)
		}
		return raw.Bytes, nil
	}
	return drisl.Marshal(prepareForMarshal(stampedForMarshal(ltd.Val)))
}

// stampedForMarshal returns val with its LexiconTypeID field (the $type field
// on generated structs) set from its RecordTypeID, so $type is always present
// in output even if the caller didn't set it. When the field needs to change,
// the record is shallow-copied first — marshaling never mutates the caller's
// value (which would be a data race for a record marshaled concurrently).
func stampedForMarshal(val Record) any {
	v := reflect.ValueOf(val)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return val
	}
	e := v.Elem()
	if e.Kind() != reflect.Struct {
		return val
	}
	f := e.FieldByName("LexiconTypeID")
	if !f.IsValid() || f.Kind() != reflect.String {
		return val
	}
	want := val.RecordTypeID()
	if f.String() == want {
		return val
	}
	cp := reflect.New(e.Type())
	cp.Elem().Set(e)
	cp.Elem().FieldByName("LexiconTypeID").SetString(want)
	return cp.Interface()
}
