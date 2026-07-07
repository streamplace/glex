// Package glexrt is the runtime support library for glex-generated lexicon
// types. It provides DAG-CBOR-native value wrappers (Link, Blob, Bytes), the
// io.Writer/io.Reader adapter helpers that let generated structs satisfy
// indigo's cbg.CBORMarshaler interface, and the $type registry / decode
// machinery used by the firehose and XRPC layers.
//
// Serialization is canonical DAG-CBOR via go-dasl (drisl), not
// whyrusleeping/cbor-gen. The cbg library is used only for low-level CID
// tag-42 and byte-string primitives (WriteCid/ReadCid/WriteByteArray), which
// are stable and produce byte-identical output to indigo's lexutil.
package glexrt

import (
	"bytes"
	"encoding/json"

	"github.com/ipfs/go-cid"
	xerrors "golang.org/x/xerrors"

	cbg "github.com/whyrusleeping/cbor-gen"
)

// Link is a DAG-CBOR CID link (`{"$link": ...}` in JSON, tag-42 in CBOR). It
// wraps github.com/ipfs/go-cid for interoperability with the rest of the
// atproto Go stack. It implements go-dasl's Marshaler/Unmarshaler
// (MarshalCBOR() []byte) so go-dasl invokes it for nested struct fields.
type Link cid.Cid

type jsonLink struct {
	Link string `json:"$link"`
}

func (l Link) String() string { return cid.Cid(l).String() }
func (l Link) Defined() bool   { return cid.Cid(l).Defined() }
func (l Link) Cid() cid.Cid    { return cid.Cid(l) }

func (l Link) MarshalJSON() ([]byte, error) {
	if !l.Defined() {
		return nil, xerrors.Errorf("tried to marshal nil or undefined cid-link")
	}
	return json.Marshal(jsonLink{Link: l.String()})
}

func (l *Link) UnmarshalJSON(raw []byte) error {
	var jl jsonLink
	if err := json.Unmarshal(raw, &jl); err != nil {
		return xerrors.Errorf("parsing cid-link JSON: %v", err)
	}
	c, err := cid.Decode(jl.Link)
	if err != nil {
		return xerrors.Errorf("parsing cid-link CID: %v", err)
	}
	*l = Link(c)
	return nil
}

// MarshalCBOR implements go-dasl's Marshaler. It emits the canonical DAG-CBOR
// tag-42 encoding, byte-identical to indigo's lexutil.LexLink (via
// cbg.WriteCid).
func (l Link) MarshalCBOR() ([]byte, error) {
	if !l.Defined() {
		return nil, xerrors.Errorf("tried to marshal nil or undefined cid-link")
	}
	var buf bytes.Buffer
	if err := cbg.WriteCid(cbg.NewCborWriter(&buf), cid.Cid(l)); err != nil {
		return nil, xerrors.Errorf("failed to write cid-link as CBOR: %w", err)
	}
	return buf.Bytes(), nil
}

// UnmarshalCBOR implements go-dasl's Unmarshaler.
func (l *Link) UnmarshalCBOR(b []byte) error {
	c, err := cbg.ReadCid(cbg.NewCborReader(bytes.NewReader(b)))
	if err != nil {
		return xerrors.Errorf("failed to read cid-link from CBOR: %w", err)
	}
	*l = Link(c)
	return nil
}
