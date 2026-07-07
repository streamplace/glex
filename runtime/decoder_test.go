package glexrt

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/hyphacoop/go-dasl/drisl"
	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
)

// TestRecord is a minimal generated-type stand-in: it has a $type field with
// a cborgen const tag and the cbg.CBORMarshalor adapter methods that the
// generator emits.
type TestRecord struct {
	LexiconTypeID string `json:"$type" cborgen:"$type,const=test.example.record"`
	Text          string `json:"text" cborgen:"text"`
	Count         *int64 `json:"count,omitempty" cborgen:"count,omitempty"`
}

func (t *TestRecord) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	t.LexiconTypeID = "test.example.record"
	return MarshalCBOR(w, t)
}

func (t *TestRecord) UnmarshalCBOR(r io.Reader) error {
	return UnmarshalCBOR(r, t)
}

func TestRegisterAndNewFromType(t *testing.T) {
	// RegisterTest is idempotent-safe: RegisterType panics on duplicate *with
	// a different type*, but same-type re-registration is a no-op.
	RegisterType("test.example.record", &TestRecord{})

	v, err := NewFromType("test.example.record")
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := v.(*TestRecord)
	if !ok {
		t.Fatalf("expected *TestRecord, got %T", v)
	}
	if tr.Text != "" {
		t.Errorf("expected zero-value Text, got %q", tr.Text)
	}

	// Unknown type
	_, err = NewFromType("nonexistent.type")
	if err == nil {
		t.Fatal("expected error for unregistered type")
	}
}

func TestCborDecodeValue(t *testing.T) {
	RegisterType("test.example.record", &TestRecord{})

	orig := &TestRecord{LexiconTypeID: "test.example.record", Text: "hello world"}
	count := int64(42)
	orig.Count = &count

	enc, err := drisl.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := CborDecodeValue(enc)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := decoded.(*TestRecord)
	if !ok {
		t.Fatalf("expected *TestRecord, got %T", decoded)
	}
	if tr.Text != "hello world" {
		t.Errorf("Text: got %q, want %q", tr.Text, "hello world")
	}
	if tr.Count == nil || *tr.Count != 42 {
		t.Errorf("Count: got %v, want 42", tr.Count)
	}
	if tr.LexiconTypeID != "test.example.record" {
		t.Errorf("LexiconTypeID: got %q, want %q", tr.LexiconTypeID, "test.example.record")
	}
}

func TestCborDecodeValueUnrecognized(t *testing.T) {
	// Encode a record with a $type that's not registered.
	raw := map[string]any{"$type": "unregistered.type", "text": "foo"}
	enc, err := drisl.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CborDecodeValue(enc)
	if err == nil {
		t.Fatal("expected ErrUnrecognizedType")
	}
}

func TestJsonDecodeValue(t *testing.T) {
	RegisterType("test.example.record", &TestRecord{})

	raw := []byte(`{"$type":"test.example.record","text":"json test","count":7}`)
	decoded, err := JsonDecodeValue(raw)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := decoded.(*TestRecord)
	if !ok {
		t.Fatalf("expected *TestRecord, got %T", decoded)
	}
	if tr.Text != "json test" {
		t.Errorf("Text: got %q, want %q", tr.Text, "json test")
	}
	if tr.Count == nil || *tr.Count != 7 {
		t.Errorf("Count: got %v, want 7", tr.Count)
	}
}

func TestLexiconTypeDecoderJSON(t *testing.T) {
	RegisterType("test.example.record", &TestRecord{})

	// Marshal a record wrapped in a decoder
	orig := &TestRecord{Text: "wrapped"}
	ltd := &LexiconTypeDecoder{Val: orig}
	out, err := ltd.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	// Should contain $type
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["$type"] != "test.example.record" {
		t.Errorf("$type: got %v, want test.example.record", m["$type"])
	}

	// Unmarshal back
	var ltd2 LexiconTypeDecoder
	if err := ltd2.UnmarshalJSON(out); err != nil {
		t.Fatal(err)
	}
	tr, ok := ltd2.Val.(*TestRecord)
	if !ok {
		t.Fatalf("expected *TestRecord, got %T", ltd2.Val)
	}
	if tr.Text != "wrapped" {
		t.Errorf("Text: got %q, want %q", tr.Text, "wrapped")
	}
}

func TestLexiconTypeDecoderCBOR(t *testing.T) {
	RegisterType("test.example.record", &TestRecord{})

	orig := &TestRecord{LexiconTypeID: "test.example.record", Text: "cbor wrapped"}
	enc, err := drisl.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}

	var ltd LexiconTypeDecoder
	if err := ltd.UnmarshalCBOR(enc); err != nil {
		t.Fatal(err)
	}
	tr, ok := ltd.Val.(*TestRecord)
	if !ok {
		t.Fatalf("expected *TestRecord, got %T", ltd.Val)
	}
	if tr.Text != "cbor wrapped" {
		t.Errorf("Text: got %q, want %q", tr.Text, "cbor wrapped")
	}

	// Re-marshal and verify byte stability
	out, err := ltd.MarshalCBOR()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(enc, out) {
		t.Errorf("CBOR roundtrip not byte-stable:\n  orig: %x\n  out:  %x", enc, out)
	}
}

func TestLexiconTypeDecoderUnrecognized(t *testing.T) {
	// JSON with unrecognized type should store raw, not error
	raw := []byte(`{"$type":"unknown.type","foo":"bar"}`)
	var ltd LexiconTypeDecoder
	if err := ltd.UnmarshalJSON(raw); err != nil {
		t.Fatal(err)
	}
	// Should round-trip
	out, err := ltd.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(raw) {
		t.Errorf("unrecognized JSON roundtrip mismatch:\n  in:  %s\n  out: %s", raw, out)
	}
}

func TestCborTypeExtractReader(t *testing.T) {
	orig := &TestRecord{LexiconTypeID: "test.example.record", Text: "reader test"}
	enc, err := drisl.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	typ, b, err := CborTypeExtractReader(bytes.NewReader(enc))
	if err != nil {
		t.Fatal(err)
	}
	if typ != "test.example.record" {
		t.Errorf("type: got %q, want test.example.record", typ)
	}
	if !bytes.Equal(enc, b) {
		t.Errorf("bytes not preserved")
	}
}

func TestLinkRoundtrip(t *testing.T) {
	c := cid.MustParse("bafkreib2rxk3rybk3aobmv5cjuql3bm2twh4jo5uxyjfxzvjcamdmc76jm")
	enc, err := drisl.Marshal(Link(c))
	if err != nil {
		t.Fatal(err)
	}
	var got Link
	if err := drisl.Unmarshal(enc, &got); err != nil {
		t.Fatal(err)
	}
	if got.String() != c.String() {
		t.Errorf("Link roundtrip: got %s, want %s", got, c)
	}
}

func TestBytesRoundtrip(t *testing.T) {
	orig := Bytes([]byte{1, 2, 3, 4, 5})
	enc, err := drisl.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got Bytes
	if err := drisl.Unmarshal(enc, &got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(orig, got) {
		t.Errorf("Bytes roundtrip: got %v, want %v", got, orig)
	}
}
