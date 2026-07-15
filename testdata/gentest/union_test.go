package comexample_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyphacoop/go-dasl/drisl"
	"github.com/streamplace/glex/testdata/gentest/comexample"
)

// TestUnionCBORFlattening locks the atproto union wire format: a union nested
// inside a record must serialize as its single set variant with a stamped
// $type — never as the Go wrapper struct. This covers all three union field
// shapes: required (value), optional (pointer), and array element.
func TestUnionCBORFlattening(t *testing.T) {
	rec := &comexample.Embed{
		CreatedAt: "2024-01-01T00:00:00Z",
		Media: comexample.Embed_Media{
			Embed_Images: &comexample.Embed_Images{Alt: "a picture"},
		},
		Fallback: &comexample.Embed_Fallback{
			Embed_External: &comexample.Embed_External{Uri: "https://example.com"},
		},
		Extras: []comexample.Embed_Extras_Elem{
			{Embed_External: &comexample.Embed_External{Uri: "https://example.com/2"}},
		},
	}

	var buf bytes.Buffer
	if err := rec.MarshalCBOR(&buf); err != nil {
		t.Fatalf("MarshalCBOR: %v", err)
	}
	enc := buf.Bytes()

	// Decode as a generic map to inspect the actual wire shape.
	var m map[string]any
	if err := drisl.Unmarshal(enc, &m); err != nil {
		t.Fatalf("generic decode: %v", err)
	}

	media, ok := m["media"].(map[string]any)
	if !ok {
		t.Fatalf("media is %T, want map", m["media"])
	}
	if media["$type"] != "com.example.embed#images" {
		t.Errorf("media $type: got %v, want com.example.embed#images", media["$type"])
	}
	if media["alt"] != "a picture" {
		t.Errorf("media alt: got %v", media["alt"])
	}
	if _, leaked := media["Embed_Images"]; leaked {
		t.Error("union serialized as Go wrapper struct, not flattened variant")
	}

	fallback, ok := m["fallback"].(map[string]any)
	if !ok {
		t.Fatalf("fallback is %T, want map", m["fallback"])
	}
	if fallback["$type"] != "com.example.embed#external" {
		t.Errorf("fallback $type: got %v, want com.example.embed#external", fallback["$type"])
	}

	extras, ok := m["extras"].([]any)
	if !ok || len(extras) != 1 {
		t.Fatalf("extras is %T len %d, want 1-element array", m["extras"], len(extras))
	}
	extra := extras[0].(map[string]any)
	if extra["$type"] != "com.example.embed#external" {
		t.Errorf("extras[0] $type: got %v, want com.example.embed#external", extra["$type"])
	}

	// Full typed round-trip: variants survive and re-marshal is byte-identical
	// (CID stability).
	var back comexample.Embed
	if err := back.UnmarshalCBOR(bytes.NewReader(enc)); err != nil {
		t.Fatalf("typed decode: %v", err)
	}
	if back.Media.Embed_Images == nil || back.Media.Embed_Images.Alt != "a picture" {
		t.Errorf("media variant lost in round-trip: %+v", back.Media)
	}
	if back.Fallback == nil || back.Fallback.Embed_External == nil || back.Fallback.Embed_External.Uri != "https://example.com" {
		t.Errorf("fallback variant lost in round-trip: %+v", back.Fallback)
	}
	if len(back.Extras) != 1 || back.Extras[0].Embed_External == nil {
		t.Fatalf("extras lost in round-trip: %+v", back.Extras)
	}

	var buf2 bytes.Buffer
	if err := back.MarshalCBOR(&buf2); err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(enc, buf2.Bytes()) {
		t.Errorf("re-marshal not byte-identical\n  first:  %x\n  second: %x", enc, buf2.Bytes())
	}
}

// TestUnionJSONFlattening is the JSON counterpart of TestUnionCBORFlattening.
func TestUnionJSONFlattening(t *testing.T) {
	rec := &comexample.Embed{
		CreatedAt: "2024-01-01T00:00:00Z",
		Media: comexample.Embed_Media{
			Embed_Images: &comexample.Embed_Images{Alt: "a picture"},
		},
	}
	bs, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(bs, &m); err != nil {
		t.Fatal(err)
	}
	media, ok := m["media"].(map[string]any)
	if !ok {
		t.Fatalf("media is %T, want map", m["media"])
	}
	if media["$type"] != "com.example.embed#images" {
		t.Errorf("media $type: got %v, want com.example.embed#images", media["$type"])
	}

	var back comexample.Embed
	if err := json.Unmarshal(bs, &back); err != nil {
		t.Fatal(err)
	}
	if back.Media.Embed_Images == nil || back.Media.Embed_Images.Alt != "a picture" {
		t.Errorf("media variant lost in JSON round-trip: %+v", back.Media)
	}
}

// TestUnionRawPreservationCBOR verifies that a union variant whose $type is
// not in the generated set is preserved verbatim (via Raw) and re-encodes
// byte-identically — an evolved lexicon must not corrupt data passing through
// an older consumer.
func TestUnionRawPreservationCBOR(t *testing.T) {
	wire := map[string]any{
		"$type":     "com.example.embed",
		"createdAt": "2024-01-01T00:00:00Z",
		"media": map[string]any{
			"$type": "com.example.future#hologram",
			"depth": int64(3),
		},
	}
	enc, err := drisl.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}

	var rec comexample.Embed
	if err := rec.UnmarshalCBOR(bytes.NewReader(enc)); err != nil {
		t.Fatalf("decode with unknown variant: %v", err)
	}
	if rec.Media.Embed_Images != nil || rec.Media.Embed_External != nil {
		t.Fatalf("unknown variant decoded into a known field: %+v", rec.Media)
	}
	if rec.Media.Raw == nil {
		t.Fatal("unknown variant was dropped instead of raw-preserved")
	}
	if rec.Media.Raw.Type != "com.example.future#hologram" {
		t.Errorf("raw $type: got %q", rec.Media.Raw.Type)
	}

	var buf bytes.Buffer
	if err := rec.MarshalCBOR(&buf); err != nil {
		t.Fatalf("re-marshal with raw variant: %v", err)
	}
	if !bytes.Equal(enc, buf.Bytes()) {
		t.Errorf("raw round-trip not byte-identical\n  first:  %x\n  second: %x", enc, buf.Bytes())
	}
}

// TestUnionRawPreservationJSON is the JSON counterpart.
func TestUnionRawPreservationJSON(t *testing.T) {
	wire := []byte(`{"$type":"com.example.embed","createdAt":"2024-01-01T00:00:00Z","media":{"$type":"com.example.future#hologram","depth":3}}`)

	var rec comexample.Embed
	if err := json.Unmarshal(wire, &rec); err != nil {
		t.Fatalf("decode with unknown variant: %v", err)
	}
	if rec.Media.Raw == nil {
		t.Fatal("unknown variant was dropped instead of raw-preserved")
	}

	out, err := json.Marshal(&rec)
	if err != nil {
		t.Fatalf("re-marshal with raw variant: %v", err)
	}
	var a, b map[string]any
	if err := json.Unmarshal(wire, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &b); err != nil {
		t.Fatal(err)
	}
	am, bm := a["media"].(map[string]any), b["media"].(map[string]any)
	if am["$type"] != bm["$type"] || am["depth"] != bm["depth"] {
		t.Errorf("raw JSON round-trip lost data: in %v, out %v", am, bm)
	}
}

// TestEmptyUnionErrors verifies an unset required union fails marshal with an
// error naming the union type, rather than emitting garbage.
func TestEmptyUnionErrors(t *testing.T) {
	rec := &comexample.Embed{CreatedAt: "2024-01-01T00:00:00Z"}
	var buf bytes.Buffer
	err := rec.MarshalCBOR(&buf)
	if err == nil {
		t.Fatal("expected error marshaling record with empty required union")
	}
	if !strings.Contains(err.Error(), "Embed_Media") {
		t.Errorf("error should name the union type, got: %v", err)
	}
	if _, err := json.Marshal(rec); err == nil {
		t.Fatal("expected error JSON-marshaling record with empty required union")
	}
}
