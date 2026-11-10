package lexicon

import (
	"encoding/json"
	"reflect"
	"testing"
)

const spaceDeclarationJSON = `{
  "lexicon": 1,
  "id": "place.stream.personal",
  "defs": {
    "main": {
      "type": "space",
      "description": "Private Streamplace records stored on the user's PDS.",
      "key": "literal:self",
      "name": "Streamplace private data",
      "name:lang": {"en": "Streamplace private data"},
      "collections": ["place.stream.multistream.target"]
    }
  }
}`

const spacePermissionSetJSON = `{
  "lexicon": 1,
  "id": "place.stream.authFull",
  "defs": {
    "main": {
      "type": "permission-set",
      "title": "Full Streamplace Access",
      "detail": "Full access to all Streamplace features and data.",
      "permissions": [
        {
          "type": "permission",
          "resource": "space",
          "spaceType": "place.stream.personal",
          "authority": "self",
          "skey": "*",
          "collection": ["place.stream.multistream.target"],
          "action": ["read", "create", "update", "delete"],
          "manage": ["create"]
        }
      ]
    }
  }
}`

func parseSchema(t *testing.T, doc string) *SchemaFile {
	t.Helper()
	var sf SchemaFile
	if err := json.Unmarshal([]byte(doc), &sf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := sf.FinishParse(); err != nil {
		t.Fatalf("FinishParse: %v", err)
	}
	if err := sf.CheckSchema(); err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	return &sf
}

func TestSpaceDeclarationRoundTrip(t *testing.T) {
	sf := parseSchema(t, spaceDeclarationJSON)

	space, ok := sf.Defs["main"].Inner.(SchemaSpace)
	if !ok {
		t.Fatalf("main def is %T, want SchemaSpace", sf.Defs["main"].Inner)
	}
	if space.Key != "literal:self" {
		t.Errorf("key: got %q", space.Key)
	}
	if space.Name != "Streamplace private data" {
		t.Errorf("name: got %q", space.Name)
	}
	if got := space.NameLang["en"]; got != "Streamplace private data" {
		t.Errorf("name:lang[en]: got %q", got)
	}
	if !reflect.DeepEqual(space.Collections, []string{"place.stream.multistream.target"}) {
		t.Errorf("collections: got %v", space.Collections)
	}

	bs, err := json.Marshal(sf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back SchemaFile
	if err := json.Unmarshal(bs, &back); err != nil {
		t.Fatalf("unmarshal round trip: %v", err)
	}
	if err := back.FinishParse(); err != nil {
		t.Fatalf("FinishParse round trip: %v", err)
	}
	if err := back.CheckSchema(); err != nil {
		t.Fatalf("CheckSchema round trip: %v", err)
	}
	if !reflect.DeepEqual(sf.Defs["main"], back.Defs["main"]) {
		t.Errorf("space def changed across round trip:\n got %#v\nwant %#v", back.Defs["main"], sf.Defs["main"])
	}

	// The raw JSON must still carry every field, including name:lang.
	var raw struct {
		Defs map[string]map[string]any `json:"defs"`
	}
	if err := json.Unmarshal(bs, &raw); err != nil {
		t.Fatal(err)
	}
	main := raw.Defs["main"]
	if main["key"] != "literal:self" || main["name"] != "Streamplace private data" {
		t.Errorf("raw round trip lost space fields: %v", main)
	}
	if langs, ok := main["name:lang"].(map[string]any); !ok || langs["en"] == nil {
		t.Errorf("raw round trip lost name:lang: %v", main["name:lang"])
	}
}

func TestSpacePermissionRoundTrip(t *testing.T) {
	sf := parseSchema(t, spacePermissionSetJSON)

	set, ok := sf.Defs["main"].Inner.(SchemaPermissionSet)
	if !ok {
		t.Fatalf("main def is %T, want SchemaPermissionSet", sf.Defs["main"].Inner)
	}
	if len(set.Permissions) != 1 {
		t.Fatalf("permissions: got %d", len(set.Permissions))
	}
	perm := set.Permissions[0]
	if perm.Resource != "space" || perm.SpaceType != "place.stream.personal" {
		t.Errorf("space permission identity: %#v", perm)
	}
	if perm.Authority != "self" || perm.SKey != "*" {
		t.Errorf("space selector: authority=%q skey=%q", perm.Authority, perm.SKey)
	}
	if !reflect.DeepEqual(perm.Collection, []string{"place.stream.multistream.target"}) {
		t.Errorf("collections: got %v", perm.Collection)
	}
	if !reflect.DeepEqual(perm.Action, []string{"read", "create", "update", "delete"}) {
		t.Errorf("actions: got %v", perm.Action)
	}
	if !reflect.DeepEqual(perm.Manage, []string{"create"}) {
		t.Errorf("manage: got %v", perm.Manage)
	}

	bs, err := json.Marshal(sf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back SchemaFile
	if err := json.Unmarshal(bs, &back); err != nil {
		t.Fatalf("unmarshal round trip: %v", err)
	}
	if err := back.FinishParse(); err != nil {
		t.Fatalf("FinishParse round trip: %v", err)
	}
	if err := back.CheckSchema(); err != nil {
		t.Fatalf("CheckSchema round trip: %v", err)
	}
	backSet := back.Defs["main"].Inner.(SchemaPermissionSet)
	if !reflect.DeepEqual(set.Permissions, backSet.Permissions) {
		t.Errorf("permission changed across round trip:\n got %#v\nwant %#v", backSet.Permissions, set.Permissions)
	}

	// Every space parameter must survive serialization by name.
	var raw struct {
		Defs map[string]struct {
			Permissions []map[string]any `json:"permissions"`
		} `json:"defs"`
	}
	if err := json.Unmarshal(bs, &raw); err != nil {
		t.Fatal(err)
	}
	got := raw.Defs["main"].Permissions[0]
	for _, field := range []string{"spaceType", "authority", "skey", "collection", "action", "manage"} {
		if _, ok := got[field]; !ok {
			t.Errorf("round trip dropped %q: %v", field, got)
		}
	}
}

func TestSpaceDefinitionValidation(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{"wildcard collection", `{"lexicon":1,"id":"com.example.space","defs":{"main":{"type":"space","key":"tid","name":"x","collections":["*"]}}}`},
		{"missing key", `{"lexicon":1,"id":"com.example.space","defs":{"main":{"type":"space","name":"x","collections":[]}}}`},
		{"bad collection", `{"lexicon":1,"id":"com.example.space","defs":{"main":{"type":"space","key":"tid","name":"x","collections":["not an nsid"]}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sf SchemaFile
			if err := json.Unmarshal([]byte(tc.doc), &sf); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if err := sf.FinishParse(); err != nil {
				t.Fatalf("FinishParse: %v", err)
			}
			if err := sf.CheckSchema(); err == nil {
				t.Fatal("CheckSchema succeeded, want error")
			}
		})
	}
}

func TestSpacePermissionValidation(t *testing.T) {
	base := `{"type":"permission","resource":"space","spaceType":"place.stream.personal"}`
	cases := []struct {
		name string
		perm string
	}{
		{"missing spaceType", `{"type":"permission","resource":"space"}`},
		{"wildcard spaceType", `{"type":"permission","resource":"space","spaceType":"*"}`},
		{"invalid spaceType", `{"type":"permission","resource":"space","spaceType":"not an nsid"}`},
		{"invalid authority", `{"type":"permission","resource":"space","spaceType":"place.stream.personal","authority":"not a did"}`},
		{"invalid skey", `{"type":"permission","resource":"space","spaceType":"place.stream.personal","skey":"not a key!"}`},
		{"invalid collection", `{"type":"permission","resource":"space","spaceType":"place.stream.personal","collection":["nope"]}`},
		{"invalid action", `{"type":"permission","resource":"space","spaceType":"place.stream.personal","action":["share"]}`},
		{"invalid manage", `{"type":"permission","resource":"space","spaceType":"place.stream.personal","manage":["read"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := SchemaPermission{}
			if err := json.Unmarshal([]byte(tc.perm), &s); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if err := s.CheckSchema(); err == nil {
				t.Fatal("CheckSchema succeeded, want error")
			}
		})
	}

	// Wildcards allowed where proposal 0016 allows them.
	s := SchemaPermission{}
	wide := `{"type":"permission","resource":"space","spaceType":"place.stream.personal","authority":"*","skey":"*","collection":["*"],"action":["read_self"],"manage":["update","delete"]}`
	if err := json.Unmarshal([]byte(wide), &s); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckSchema(); err != nil {
		t.Fatalf("wildcard permission rejected: %v", err)
	}
	if err := json.Unmarshal([]byte(base), &s); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckSchema(); err != nil {
		t.Fatalf("minimal space permission rejected: %v", err)
	}
}

func TestSpaceRefFormat(t *testing.T) {
	format := "space-ref"
	valid := SchemaString{Type: "string", Format: &format}
	validDoc := `{"lexicon":1,"id":"com.example.spaceCall","defs":{"main":{"type":"procedure","input":{"encoding":"application/json","schema":{"type":"object","required":["space"],"properties":{"space":{"type":"string","format":"space-ref"}}}}}}}`
	parseSchema(t, validDoc)
	if err := valid.Validate("at://did:plc:abc123/space/com.example.space/self", 0); err != nil {
		t.Fatalf("valid space-ref rejected: %v", err)
	}
	if err := valid.Validate("at://did:plc:abc123/com.example.space/self", 0); err == nil {
		t.Fatal("space-ref without /space/ segment accepted")
	}
	if err := valid.Validate("at://not-a-did/space/com.example.space/self", 0); err == nil {
		t.Fatal("space-ref with non-DID authority accepted")
	}

	invalidDoc := `{"lexicon":1,"id":"com.example.spaceCall","defs":{"main":{"type":"procedure","input":{"encoding":"application/json","schema":{"type":"object","properties":{"space":{"type":"string","format":"space-ref!"}}}}}}}`
	var sf SchemaFile
	if err := json.Unmarshal([]byte(invalidDoc), &sf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := sf.FinishParse(); err != nil {
		t.Fatalf("FinishParse: %v", err)
	}
	if err := sf.CheckSchema(); err == nil {
		t.Fatal("unknown format accepted")
	}
}
