package glex

import (
	"encoding/json"
	"io"

	daslcid "github.com/hyphacoop/go-dasl/cid"
	"github.com/hyphacoop/go-dasl/drisl"
)

// MarshalCBOR encodes v as canonical DAG-CBOR (via go-dasl) and writes it to w.
// Generated record/object types call this from their cbg.CBORMarshalor-shaped
// MarshalCBOR(io.Writer) adapter, so they interoperate with indigo's
// repo/carstore/MST layer while serializing through go-dasl.
func MarshalCBOR(w io.Writer, v any) error {
	b, err := drisl.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// UnmarshalCBOR reads canonical DAG-CBOR from r and decodes it into v (via
// go-dasl).
func UnmarshalCBOR(r io.Reader, v any) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return drisl.Unmarshal(b, v)
}

// MarshalCBORBytes encodes v as canonical DAG-CBOR and returns the bytes.
// Generated union types call this from their drisl-shaped MarshalCBOR.
func MarshalCBORBytes(v any) ([]byte, error) {
	return drisl.Marshal(v)
}

// CidForRecord computes the canonical DAG-CBOR CID for a generated record,
// stamping $type the same way marshaling does (without mutating rec). Use
// this instead of drisl.CidForValue, which encodes the record as-is and would
// silently omit $type when the caller never set LexiconTypeID.
func CidForRecord(rec Record) (daslcid.Cid, error) {
	return drisl.CidForValue(stampedForMarshal(rec))
}

// UnmarshalCBORBytes decodes canonical DAG-CBOR bytes into v. Generated union
// types call this from their drisl-shaped UnmarshalCBOR.
func UnmarshalCBORBytes(b []byte, v any) error {
	return drisl.Unmarshal(b, v)
}

// typeHolder is used to extract just the $type field from a JSON or CBOR
// record for union/registry dispatch.
type typeHolder struct {
	Type string `json:"$type"`
}

// typeExtractJSON extracts the $type string from a JSON record.
func typeExtractJSON(b []byte) (string, error) {
	var th typeHolder
	if err := json.Unmarshal(b, &th); err != nil {
		return "", err
	}
	return th.Type, nil
}

// typeExtractCBOR extracts the $type string from a DAG-CBOR record.
func typeExtractCBOR(b []byte) (string, error) {
	var th typeHolder
	if err := drisl.Unmarshal(b, &th); err != nil {
		return "", err
	}
	return th.Type, nil
}

// TypeExtract extracts the $type string from a JSON record (exported for
// generated code and consumers).
func TypeExtract(b []byte) (string, error) {
	return typeExtractJSON(b)
}

// CborTypeExtract extracts the $type string from a DAG-CBOR record.
func CborTypeExtract(b []byte) (string, error) {
	return typeExtractCBOR(b)
}

// CborTypeExtractReader reads a full DAG-CBOR record from r, extracts the
// $type string, and returns both the type and the full bytes read. The bytes
// can then be decoded into a concrete type after dispatch.
func CborTypeExtractReader(r io.Reader) (string, []byte, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", nil, err
	}
	typ, err := typeExtractCBOR(b)
	if err != nil {
		return "", nil, err
	}
	return typ, b, nil
}
