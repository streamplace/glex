package comexample_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/hyphacoop/go-dasl/drisl"
	glex "github.com/streamplace/glex/runtime"
	"github.com/streamplace/glex/testdata/gentest/comatproto"
)

// recordingClient implements glex.LexClient and records the call, filling the
// out pointer with a canned response.
type recordingClient struct {
	method   string
	encoding string
	endpoint string
	params   map[string]any
	body     any
	resp     any
}

func (c *recordingClient) LexDo(ctx context.Context, method string, inputEncoding string, endpoint string,
	params map[string]any, bodyData any, out any) error {
	c.method = method
	c.encoding = inputEncoding
	c.endpoint = endpoint
	c.params = params
	c.body = bodyData
	if c.resp != nil {
		bs, err := json.Marshal(c.resp)
		if err != nil {
			return err
		}
		return json.Unmarshal(bs, out)
	}
	return nil
}

func TestSpaceEndpoints(t *testing.T) {
	ctx := context.Background()

	skey := "self"
	createIn := &comatproto.SpaceCreateSpace_Input{
		SpaceType: "place.stream.personal",
		Skey:      &skey,
		Policy: comatproto.SpaceCreateSpace_Input_Policy{
			PersonalspacePolicy: &comatproto.PersonalspacePolicy{},
		},
	}
	createClient := &recordingClient{
		resp: map[string]any{"uri": "at://did:plc:abc123/space/place.stream.personal/self"},
	}
	out, err := comatproto.SpaceCreateSpace(ctx, createClient, createIn)
	if err != nil {
		t.Fatalf("SpaceCreateSpace: %v", err)
	}
	if createClient.method != glex.Procedure || createClient.encoding != "application/json" {
		t.Errorf("createSpace call: method=%q encoding=%q", createClient.method, createClient.encoding)
	}
	if createClient.endpoint != "com.atproto.space.createSpace" {
		t.Errorf("createSpace endpoint: %q", createClient.endpoint)
	}
	if createClient.params != nil {
		t.Errorf("createSpace params: %v", createClient.params)
	}
	if out.Uri != "at://did:plc:abc123/space/place.stream.personal/self" {
		t.Errorf("createSpace output: %#v", out)
	}

	// The policy union must carry its $type on the wire.
	policyJSON, err := json.Marshal(createIn.Policy)
	if err != nil {
		t.Fatal(err)
	}
	var policyDoc map[string]any
	if err := json.Unmarshal(policyJSON, &policyDoc); err != nil {
		t.Fatal(err)
	}
	if policyDoc["$type"] != "com.atproto.personalspace.policy" {
		t.Errorf("policy union $type: %v", policyDoc["$type"])
	}

	putClient := &recordingClient{
		resp: map[string]any{
			"uri": "at://did:plc:abc123/space/place.stream.personal/self/did:plc:abc123/place.stream.multistream.target/1",
			"cid": "bafyreib2rxk3rh6kzwq",
		},
	}
	record, err := glex.Unknown(map[string]any{"url": "rtmp://example.com/live"})
	if err != nil {
		t.Fatal(err)
	}
	putOut, err := comatproto.SpacePutRecord(ctx, putClient, &comatproto.SpacePutRecord_Input{
		Space:      "at://did:plc:abc123/space/place.stream.personal/self",
		Repo:       "did:plc:abc123",
		Collection: "place.stream.multistream.target",
		Rkey:       "1",
		Record:     record,
	})
	if err != nil {
		t.Fatalf("SpacePutRecord: %v", err)
	}
	if putClient.endpoint != "com.atproto.space.putRecord" || putClient.method != glex.Procedure {
		t.Errorf("putRecord call: method=%q endpoint=%q", putClient.method, putClient.endpoint)
	}
	if putOut.Cid != "bafyreib2rxk3rh6kzwq" {
		t.Errorf("putRecord output: %#v", putOut)
	}

	delClient := &recordingClient{}
	delOut, err := comatproto.SpaceDeleteRecord(ctx, delClient, &comatproto.SpaceDeleteRecord_Input{
		Space:      "at://did:plc:abc123/space/place.stream.personal/self",
		Repo:       "did:plc:abc123",
		Collection: "place.stream.multistream.target",
		Rkey:       "1",
	})
	if err != nil {
		t.Fatalf("SpaceDeleteRecord: %v", err)
	}
	if delOut == nil || delClient.endpoint != "com.atproto.space.deleteRecord" {
		t.Errorf("deleteRecord call: out=%#v method=%q endpoint=%q", delOut, delClient.method, delClient.endpoint)
	}
}

func TestSpaceRecordCBOR(t *testing.T) {
	raw, err := drisl.Marshal(map[string]any{
		"$type": "place.stream.multistream.target",
		"url":   "rtmp://example.com/live",
	})
	if err != nil {
		t.Fatal(err)
	}
	record := &glex.LexiconTypeDecoder{Val: &glex.RawRecord{
		Type:     "place.stream.multistream.target",
		Encoding: "cbor",
		Bytes:    raw,
	}}
	in := &comatproto.SpacePutRecord_Input{
		Space:      "at://did:plc:abc123/space/place.stream.personal/self",
		Repo:       "did:plc:abc123",
		Collection: "place.stream.multistream.target",
		Rkey:       "1",
		Record:     record,
	}
	var buf bytes.Buffer
	if err := in.MarshalCBOR(&buf); err != nil {
		t.Fatalf("MarshalCBOR: %v", err)
	}
	var decoded map[string]any
	if err := drisl.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded["space"] != in.Space {
		t.Errorf("space: got %v", decoded["space"])
	}
	if decoded["$type"] != "com.atproto.space.putRecord" {
		t.Errorf("$type: got %v", decoded["$type"])
	}

	// Decode back into the generated input type and verify the nested unknown
	// record survives, not just the top-level fields.
	var decodedIn comatproto.SpacePutRecord_Input
	if err := glex.UnmarshalCBORBytes(buf.Bytes(), &decodedIn); err != nil {
		t.Fatalf("UnmarshalCBORBytes: %v", err)
	}
	if decodedIn.Space != in.Space || decodedIn.Repo != in.Repo ||
		decodedIn.Collection != in.Collection || decodedIn.Rkey != in.Rkey {
		t.Errorf("round-trip input fields: %#v", decodedIn)
	}
	if decodedIn.Record == nil {
		t.Fatal("round-trip dropped the nested record")
	}
	rawRec, ok := decodedIn.Record.Val.(*glex.RawRecord)
	if !ok {
		t.Fatalf("nested record value is %T, want *glex.RawRecord", decodedIn.Record.Val)
	}
	if rawRec.Type != "place.stream.multistream.target" || rawRec.Encoding != "cbor" {
		t.Errorf("nested record identity: type=%q encoding=%q", rawRec.Type, rawRec.Encoding)
	}
	var recordDoc map[string]any
	if err := drisl.Unmarshal(rawRec.Bytes, &recordDoc); err != nil {
		t.Fatalf("decode nested record: %v", err)
	}
	if recordDoc["url"] != "rtmp://example.com/live" {
		t.Errorf("nested record url: got %v", recordDoc["url"])
	}

	// Re-encoding the decoded value must be byte-identical (stable CID).
	var roundTrip bytes.Buffer
	if err := decodedIn.MarshalCBOR(&roundTrip); err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), roundTrip.Bytes()) {
		t.Errorf("re-marshal not byte-identical\n first: %x\nsecond: %x", buf.Bytes(), roundTrip.Bytes())
	}
}
