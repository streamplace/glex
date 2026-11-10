package installer

import (
	"slices"
	"testing"
)

func refsFor(t *testing.T, doc string) []string {
	t.Helper()
	parsed, err := parseJSON([]byte(doc))
	if err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	m, ok := parsed.(*orderedMap)
	if !ok {
		t.Fatal("document is not an object")
	}
	refs, err := documentNsidRefs(m)
	if err != nil {
		t.Fatalf("documentNsidRefs: %v", err)
	}
	out := make([]string, len(refs))
	for i, ref := range refs {
		out[i] = ref.String()
	}
	return out
}

func TestSpaceDefinitionRefs(t *testing.T) {
	doc := `{
	  "lexicon": 1,
	  "id": "place.stream.personal",
	  "defs": {
	    "main": {
	      "type": "space",
	      "key": "literal:self",
	      "name": "Streamplace private data",
	      "collections": ["place.stream.multistream.target", "place.stream.video"]
	    }
	  }
	}`
	got := refsFor(t, doc)
	want := []string{"place.stream.multistream.target", "place.stream.video"}
	if !slices.Equal(got, want) {
		t.Errorf("refs: got %v, want %v", got, want)
	}
}

func TestSpacePermissionRefs(t *testing.T) {
	doc := `{
	  "lexicon": 1,
	  "id": "place.stream.authFull",
	  "defs": {
	    "main": {
	      "type": "permission-set",
	      "permissions": [
	        {"type": "permission", "resource": "repo", "collection": ["place.stream.video"]},
	        {
	          "type": "permission",
	          "resource": "space",
	          "spaceType": "place.stream.personal",
	          "authority": "self",
	          "collection": ["place.stream.multistream.target", "*"]
	        }
	      ]
	    }
	  }
	}`
	got := refsFor(t, doc)
	want := []string{"place.stream.video", "place.stream.personal", "place.stream.multistream.target"}
	if !slices.Equal(got, want) {
		t.Errorf("refs: got %v, want %v", got, want)
	}
}
