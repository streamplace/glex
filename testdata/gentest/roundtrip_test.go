package comexample_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/hyphacoop/go-dasl/drisl"
	glex "github.com/streamplace/glex/runtime"
	"github.com/streamplace/glex/testdata/gentest/comexample"
)

// TestRoundtripCIDStability verifies the core glex promise: a generated record
// marshals as canonical DAG-CBOR, decodes correctly via the $type registry
// (CborDecodeValue), and re-marshals to byte-identical output (CID stability).
func TestRoundtripCIDStability(t *testing.T) {
	// The generated Post type should have registered itself via init().
	// Verify it's in the registry.
	post := &comexample.Post{
		Text:      "hello glex",
		CreatedAt: "2024-01-01T00:00:00Z",
	}

	// Marshal via the cbg adapter (what indigo's repo/MST layer calls)
	var buf bytes.Buffer
	if err := post.MarshalCBOR(&buf); err != nil {
		t.Fatalf("MarshalCBOR: %v", err)
	}
	enc := buf.Bytes()
	t.Logf("encoded %d bytes: %x", len(enc), enc)

	// Verify $type is in the encoded bytes by decoding via the registry
	decoded, err := glex.CborDecodeValue(enc)
	if err != nil {
		t.Fatalf("CborDecodeValue: %v", err)
	}
	post2, ok := decoded.(*comexample.Post)
	if !ok {
		t.Fatalf("expected *comexample.Post, got %T", decoded)
	}
	if post2.Text != post.Text {
		t.Errorf("Text: got %q, want %q", post2.Text, post.Text)
	}
	if post2.LexiconTypeID != "com.example.post" {
		t.Errorf("LexiconTypeID: got %q, want %q", post2.LexiconTypeID, "com.example.post")
	}

	// Re-marshal and verify byte-stability (this is what gives stable CIDs)
	var buf2 bytes.Buffer
	if err := post2.MarshalCBOR(&buf2); err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(enc, buf2.Bytes()) {
		t.Errorf("CID stability: re-marshal not byte-identical\n  first:  %x\n  second: %x", enc, buf2.Bytes())
	}

	// Verify CID is stable by computing it directly
	cid1, err := drisl.CidForValue(post)
	if err != nil {
		t.Fatalf("CidForValue: %v", err)
	}
	cid2, err := drisl.CidForValue(post2)
	if err != nil {
		t.Fatalf("CidForValue (2): %v", err)
	}
	if cid1.String() != cid2.String() {
		t.Errorf("CID mismatch: %s vs %s", cid1, cid2)
	}
	t.Logf("CID: %s", cid1)
}

// TestRegistryInit verifies that generated types self-register via init().
func TestRegistryInit(t *testing.T) {
	types := glex.RegisteredTypes()
	found := false
	for _, id := range types {
		if id == "com.example.post" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("com.example.post not in registry; registered: %v", types)
	}
}

// TestDecodeUnknownType verifies that decoding a record with an unregistered
// $type returns ErrUnrecognizedType.
func TestDecodeUnknownType(t *testing.T) {
	raw := map[string]any{"$type": "nonexistent.type", "text": "foo"}
	enc, err := drisl.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = glex.CborDecodeValue(enc)
	if err == nil {
		t.Fatal("expected ErrUnrecognizedType")
	}
}

// TestJSONTypeStamping locks the $type-on-JSON contract: records (and
// standalone main objects) stamp their $type on json.Marshal without the
// caller setting LexiconTypeID, while input/output types with an unset
// LexiconTypeID omit $type entirely — never an invalid `"$type": ""`.
func TestJSONTypeStamping(t *testing.T) {
	post := &comexample.Post{Text: "stamp me", CreatedAt: "2024-01-01T00:00:00Z"}
	bs, err := json.Marshal(post)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(bs, &m); err != nil {
		t.Fatal(err)
	}
	if m["$type"] != "com.example.post" {
		t.Errorf("record $type: got %v, want com.example.post", m["$type"])
	}

	view := &comexample.InteractionView{Subject: comexample.Post{Text: "s", CreatedAt: "2024-01-01T00:00:00Z"}}
	bs, err = json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	m = map[string]any{}
	if err := json.Unmarshal(bs, &m); err != nil {
		t.Fatal(err)
	}
	if m["$type"] != "com.example.interactionView" {
		t.Errorf("main-object $type: got %v, want com.example.interactionView", m["$type"])
	}

	// An endpoint input with unset LexiconTypeID must omit $type, not emit "".
	in := &comexample.CreateLike_Input{Uri: "at://did:plc:x/com.example.post/1"}
	bs, err = json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	m = map[string]any{}
	if err := json.Unmarshal(bs, &m); err != nil {
		t.Fatal(err)
	}
	if _, present := m["$type"]; present {
		t.Errorf("input $type should be omitted when unset, got %v", m["$type"])
	}
}
