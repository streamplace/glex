package glex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyphacoop/go-dasl/drisl"
)

// TestBlobDecodeApiBlobRefShape locks the fix for the startLivestream prod
// outage: @atproto/api's BlobRef class serialized through a non-lex-aware
// stringifier (e.g. @atproto/lex's lexStringify, which only recognizes its
// OWN BlobRef class) produces a modern-shaped blob without $type. That must
// decode as a modern blob — previously it fell into the legacy branch and
// died on "parsing CID in legacy blob: invalid cid: cid too short".
func TestBlobDecodeApiBlobRefShape(t *testing.T) {
	wire := `{"ref":{"$link":"bafkreib2rxk3rybk3aobmv5cjuql3bm2twh4jo5uxyjfxzvjcamdmc76jm"},"mimeType":"image/jpeg","size":4096,"original":{"$type":"blob","ref":{"$link":"bafkreib2rxk3rybk3aobmv5cjuql3bm2twh4jo5uxyjfxzvjcamdmc76jm"},"mimeType":"image/jpeg","size":4096}}`
	var b Blob
	if err := json.Unmarshal([]byte(wire), &b); err != nil {
		t.Fatalf("decoding $type-less modern blob: %v", err)
	}
	if b.MimeType != "image/jpeg" || b.Size != 4096 {
		t.Errorf("blob fields: %+v", b)
	}
	if b.Ref.String() != "bafkreib2rxk3rybk3aobmv5cjuql3bm2twh4jo5uxyjfxzvjcamdmc76jm" {
		t.Errorf("blob ref: %s", b.Ref)
	}
}

// TestBlobDecodeNullAndGarbage: null is a no-op (encoding/json convention,
// hit when a blob sits in value position), and a non-blob object gets a
// descriptive error instead of a misleading CID-parse failure.
func TestBlobDecodeNullAndGarbage(t *testing.T) {
	var b Blob
	if err := json.Unmarshal([]byte(`null`), &b); err != nil {
		t.Errorf("null blob JSON should be a no-op, got %v", err)
	}
	if err := b.UnmarshalCBOR([]byte{0xf6}); err != nil {
		t.Errorf("null blob CBOR should be a no-op, got %v", err)
	}

	err := json.Unmarshal([]byte(`{}`), &b)
	if err == nil {
		t.Fatal("empty object should not decode as a blob")
	}
	if !strings.Contains(err.Error(), "is not a blob") {
		t.Errorf("want descriptive not-a-blob error, got: %v", err)
	}

	// Genuine legacy blobs still decode.
	legacy := `{"cid":"bafkreib2rxk3rybk3aobmv5cjuql3bm2twh4jo5uxyjfxzvjcamdmc76jm","mimeType":"image/png"}`
	var lb Blob
	if err := json.Unmarshal([]byte(legacy), &lb); err != nil {
		t.Fatalf("legacy blob: %v", err)
	}
	if lb.Size != -1 || lb.MimeType != "image/png" {
		t.Errorf("legacy blob fields: %+v", lb)
	}
}

// TestValueTypesNullNoOp covers the remaining custom unmarshalers: null must
// never be a hard error in value position.
func TestValueTypesNullNoOp(t *testing.T) {
	var l Link
	if err := json.Unmarshal([]byte(`null`), &l); err != nil {
		t.Errorf("null Link JSON: %v", err)
	}
	if err := l.UnmarshalCBOR([]byte{0xf6}); err != nil {
		t.Errorf("null Link CBOR: %v", err)
	}
	var bs Bytes
	if err := json.Unmarshal([]byte(`null`), &bs); err != nil {
		t.Errorf("null Bytes JSON: %v", err)
	}
	if err := bs.UnmarshalCBOR([]byte{0xf6}); err != nil {
		t.Errorf("null Bytes CBOR: %v", err)
	}
	var ltd LexiconTypeDecoder
	if err := json.Unmarshal([]byte(`null`), &ltd); err != nil {
		t.Errorf("null LTD JSON: %v", err)
	}
	if ltd.Val != nil {
		t.Errorf("null LTD should leave Val nil, got %#v", ltd.Val)
	}
	if err := ltd.UnmarshalCBOR([]byte{0xf6}); err != nil {
		t.Errorf("null LTD CBOR: %v", err)
	}
}

// TestBlobCBORLenient mirrors the JSON leniency for CBOR: a modern-shaped
// blob without $type decodes; a non-blob map errors descriptively.
func TestBlobCBORLenient(t *testing.T) {
	full := Blob{}
	if err := json.Unmarshal([]byte(`{"$type":"blob","ref":{"$link":"bafkreib2rxk3rybk3aobmv5cjuql3bm2twh4jo5uxyjfxzvjcamdmc76jm"},"mimeType":"image/jpeg","size":9}`), &full); err != nil {
		t.Fatal(err)
	}
	enc, err := full.MarshalCBOR()
	if err != nil {
		t.Fatal(err)
	}
	// Strip $type by re-encoding through a generic map.
	var m map[string]any
	if err := drisl.Unmarshal(enc, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "$type")
	stripped, err := drisl.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var b Blob
	if err := b.UnmarshalCBOR(stripped); err != nil {
		t.Fatalf("decoding $type-less modern blob CBOR: %v", err)
	}
	if b.Size != 9 || b.MimeType != "image/jpeg" {
		t.Errorf("blob fields: %+v", b)
	}

	empty, _ := drisl.Marshal(map[string]any{})
	if err := b.UnmarshalCBOR(empty); err == nil || !strings.Contains(err.Error(), "is not a blob") {
		t.Errorf("want descriptive not-a-blob CBOR error, got: %v", err)
	}
}
