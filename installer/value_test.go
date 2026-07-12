package installer

import (
	"encoding/hex"
	"testing"
)

// TestStringifyJSONMatchesJavaScript checks the JSON.stringify(v, null, 2)
// emulation against strings produced by Node.js.
func TestStringifyJSONMatchesJavaScript(t *testing.T) {
	m := newOrderedMap()
	m.set("id", "com.example.test")
	m.set("count", int64(42))
	m.set("nested", func() *orderedMap {
		n := newOrderedMap()
		n.set("b-first", true) // insertion order, not alphabetical
		n.set("a-second", nil)
		return n
	}())
	m.set("empty", newOrderedMap())
	m.set("list", []any{"one", int64(2)})
	m.set("escapes", "quote:\" backslash:\\ tab:\t newline:\n ctrl:\x01 unicode:é")

	want := `{
  "id": "com.example.test",
  "count": 42,
  "nested": {
    "b-first": true,
    "a-second": null
  },
  "empty": {},
  "list": [
    "one",
    2
  ],
  "escapes": "quote:\" backslash:\\ tab:\t newline:\n ctrl:\u0001 unicode:é"
}`
	if got := stringifyJSON(m); got != want {
		t.Errorf("stringifyJSON mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// TestDecodeCBORPreservesMapOrder decodes a DAG-CBOR map and checks that key
// order and tag-42 CID links survive into the JSON rendering.
func TestDecodeCBORPreservesMapOrder(t *testing.T) {
	// {"zz": 1, "a": {"/": bafyreif...}} — deliberately NOT sorted
	// alphabetically ("zz" first) to prove order is taken from the bytes.
	cidBytes, err := hex.DecodeString("0001711220b085ca19ab41b1f7bb4a316b0de952d5d691e11e3f9d0663957ad3a17ba1c66d")
	if err != nil {
		t.Fatal(err)
	}
	enc := []byte{0xa2, 0x62, 'z', 'z', 0x01, 0x61, 'a', 0xd8, 0x2a, 0x58, byte(len(cidBytes))}
	enc = append(enc, cidBytes...)

	v, err := decodeCBOR(enc)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "zz": 1,
  "a": {
    "$link": "bafyreifqqxfbtk2bwh33wsrrnmg6suwv22i6chr7tudghfl22oqxxiognu"
  }
}`
	if got := stringifyJSON(v); got != want {
		t.Errorf("decodeCBOR order mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// TestManifestStringifyNormalizes checks lexicon sorting, resolution key
// sorting, and the uri-before-cid field order used by @atproto/lex.
func TestManifestStringifyNormalizes(t *testing.T) {
	m := &Manifest{
		Version:  1,
		Lexicons: []string{"com.example.b", "com.example.a"},
		Resolutions: map[string]Resolution{
			"com.example.b": {URI: "at://did:plc:x/com.atproto.lexicon.schema/com.example.b", CID: "bafyb"},
			"com.example.a": {URI: "at://did:plc:x/com.atproto.lexicon.schema/com.example.a", CID: "bafya"},
		},
	}
	want := `{
  "version": 1,
  "lexicons": [
    "com.example.a",
    "com.example.b"
  ],
  "resolutions": {
    "com.example.a": {
      "uri": "at://did:plc:x/com.atproto.lexicon.schema/com.example.a",
      "cid": "bafya"
    },
    "com.example.b": {
      "uri": "at://did:plc:x/com.atproto.lexicon.schema/com.example.b",
      "cid": "bafyb"
    }
  }
}`
	if got := m.stringify(); got != want {
		t.Errorf("manifest stringify mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}
