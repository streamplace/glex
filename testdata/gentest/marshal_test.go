package comexample_test

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"

	"github.com/hyphacoop/go-dasl/drisl"
	"github.com/streamplace/glex/testdata/gentest/comexample"
)

// TestValueMarshalStampsType verifies json.Marshal of a record VALUE (not a
// pointer) still emits $type. With a pointer-receiver MarshalJSON this
// silently fell back to default struct marshaling and $type was dropped.
func TestValueMarshalStampsType(t *testing.T) {
	rec := comexample.Post{Text: "by value", CreatedAt: "2024-01-01T00:00:00Z"}
	bs, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(bs, &m); err != nil {
		t.Fatal(err)
	}
	if m["$type"] != "com.example.post" {
		t.Errorf("value-marshal $type: got %v, want com.example.post", m["$type"])
	}
}

// TestRequiredNilArrayEncodesEmpty verifies that an unset required array
// field encodes as an empty CBOR array (matching cbor-gen), not null — a
// cbor-gen consumer fails with "expected cbor array" on null, which broke
// subscribeRepos interop with indigo.
func TestRequiredNilArrayEncodesEmpty(t *testing.T) {
	rec := &comexample.Embed{
		CreatedAt: "2024-01-01T00:00:00Z",
		Media: comexample.Embed_Media{
			Embed_Images: &comexample.Embed_Images{Alt: "no urls set"},
		},
	}
	var buf bytes.Buffer
	if err := rec.MarshalCBOR(&buf); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := drisl.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	media := m["media"].(map[string]any)
	if _, ok := media["urls"].([]any); !ok {
		t.Errorf("required nil array inside union variant: got %T (%v), want empty array", media["urls"], media["urls"])
	}
	if rec.Media.Embed_Images.Urls != nil {
		t.Error("marshal mutated the record's nil slice")
	}
}

// TestMarshalDoesNotMutate verifies that neither JSON nor CBOR marshaling
// writes to the record being marshaled — stamping $type must happen on a
// copy, or concurrent marshals of a shared record are a data race.
func TestMarshalDoesNotMutate(t *testing.T) {
	rec := &comexample.Embed{
		CreatedAt: "2024-01-01T00:00:00Z",
		Media: comexample.Embed_Media{
			Embed_Images: &comexample.Embed_Images{Alt: "shared"},
		},
	}
	if _, err := json.Marshal(rec); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := rec.MarshalCBOR(&buf); err != nil {
		t.Fatal(err)
	}
	if rec.LexiconTypeID != "" {
		t.Errorf("marshal mutated record LexiconTypeID to %q", rec.LexiconTypeID)
	}
	if rec.Media.Embed_Images.LexiconTypeID != "" {
		t.Errorf("marshal mutated union variant LexiconTypeID to %q", rec.Media.Embed_Images.LexiconTypeID)
	}

	// Concurrent marshals of the same record must be race-free (run with
	// -race to enforce).
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = json.Marshal(rec)
			var b bytes.Buffer
			_ = rec.MarshalCBOR(&b)
		}()
	}
	wg.Wait()
}
