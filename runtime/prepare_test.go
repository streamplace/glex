package glex

import (
	"bytes"
	"testing"

	"github.com/hyphacoop/go-dasl/drisl"
)

type prepInner struct {
	Names []string `json:"names"`
}

type prepOuter struct {
	Required []string          `json:"required"`
	Optional []string          `json:"optional,omitempty"`
	Counts   map[string]int64  `json:"counts"`
	Inner    prepInner         `json:"inner"`
	InnerPtr *prepInner        `json:"innerPtr,omitempty"`
	Items    []prepInner       `json:"items"`
}

// TestPrepareForMarshalNilContainers locks the cbor-gen-compatible encoding of
// nil containers: a nil slice/map in a required (non-omitempty) field encodes
// as an empty array/map — never CBOR null, which cbor-gen consumers reject
// with "expected cbor array". Optional nil containers stay omitted.
func TestPrepareForMarshalNilContainers(t *testing.T) {
	v := &prepOuter{
		InnerPtr: &prepInner{},
		Items:    []prepInner{{}},
	}
	enc, err := drisl.Marshal(prepareForMarshal(v))
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]any
	if err := drisl.Unmarshal(enc, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["required"].([]any); !ok {
		t.Errorf("required nil slice: got %T (%v), want empty array", m["required"], m["required"])
	}
	if _, present := m["optional"]; present {
		t.Errorf("optional nil slice should be omitted, got %v", m["optional"])
	}
	if _, ok := m["counts"].(map[string]any); !ok {
		t.Errorf("required nil map: got %T (%v), want empty map", m["counts"], m["counts"])
	}
	// Nested fixes: value field, pointer field, slice element.
	inner := m["inner"].(map[string]any)
	if _, ok := inner["names"].([]any); !ok {
		t.Errorf("nested value struct: names is %T, want empty array", inner["names"])
	}
	innerPtr := m["innerPtr"].(map[string]any)
	if _, ok := innerPtr["names"].([]any); !ok {
		t.Errorf("nested pointer struct: names is %T, want empty array", innerPtr["names"])
	}
	item := m["items"].([]any)[0].(map[string]any)
	if _, ok := item["names"].([]any); !ok {
		t.Errorf("slice element struct: names is %T, want empty array", item["names"])
	}

	// The caller's value must not have been mutated.
	if v.Required != nil || v.Counts != nil || v.Inner.Names != nil || v.InnerPtr.Names != nil || v.Items[0].Names != nil {
		t.Error("prepareForMarshal mutated the input value")
	}

	// Exact-bytes check for the simple shape cbor-gen would produce:
	// {"names": []} == a1 65 6e616d6573 80
	encInner, err := drisl.Marshal(prepareForMarshal(&prepInner{}))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0xa1, 0x65, 'n', 'a', 'm', 'e', 's', 0x80}
	if !bytes.Equal(encInner, want) {
		t.Errorf("empty-required-array bytes: got %x, want %x", encInner, want)
	}
}

// TestPrepareForMarshalNoChange verifies the fast path: a value with no nil
// required containers is returned as-is (no copies).
func TestPrepareForMarshalNoChange(t *testing.T) {
	v := &prepOuter{
		Required: []string{"a"},
		Counts:   map[string]int64{},
		Inner:    prepInner{Names: []string{}},
		Items:    []prepInner{},
	}
	if got := prepareForMarshal(v); got.(*prepOuter) != v {
		t.Error("expected the same pointer back when nothing needs fixing")
	}
}
