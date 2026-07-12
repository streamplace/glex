package installer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const lexiconCollection = "com.atproto.lexicon.schema"

// resolvedLexicon is a lexicon record fetched (or reused) with its identity:
// the parsed document (with original key order), its id, and its CID.
type resolvedLexicon struct {
	doc *orderedMap
	id  string
	cid string
}

// resolver fetches lexicon records from the AT Protocol network the same way
// @atproto/lex-resolver does: `_lexicon` DNS TXT lookup for the authority
// DID, DID document resolution for the PDS endpoint and signing key, then a
// com.atproto.sync.getRecord fetch whose CAR proof (commit signature + MST
// inclusion) is verified before the record is trusted.
type resolver struct {
	dir    identity.Directory
	base   *identity.BaseDirectory
	client *http.Client
}

func newResolver() *resolver {
	base := &identity.BaseDirectory{}
	return &resolver{
		dir:    base,
		base:   base,
		client: http.DefaultClient,
	}
}

// resolveNSID finds the AT URI for a lexicon NSID via the authority's
// `_lexicon.<authority>` DNS TXT record.
func (r *resolver) resolveNSID(ctx context.Context, nsid syntax.NSID) (syntax.ATURI, error) {
	did, err := r.base.ResolveNSID(ctx, nsid)
	if err != nil {
		return "", fmt.Errorf("failed to resolve lexicon DID authority for %s: %w", nsid, err)
	}
	return syntax.ATURI(fmt.Sprintf("at://%s/%s/%s", did, lexiconCollection, nsid)), nil
}

// fetch retrieves the lexicon record at uri from its PDS with proof
// verification, mirroring LexResolver.fetchLexiconUri.
func (r *resolver) fetch(ctx context.Context, uri syntax.ATURI, noCache bool) (*resolvedLexicon, error) {
	nsid, err := syntax.ParseNSID(uri.RecordKey().String())
	if err != nil {
		return nil, fmt.Errorf("invalid lexicon record key in %s: %w", uri, err)
	}
	did, err := syntax.ParseDID(uri.Authority().String())
	if err != nil {
		return nil, fmt.Errorf("URI host is not a DID %s: %w", uri, err)
	}

	ident, err := r.dir.LookupDID(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve DID document for %s: %w", did, err)
	}
	pds := ident.PDSEndpoint()
	if pds == "" {
		return nil, fmt.Errorf("no atproto PDS service endpoint found in %s DID document", did)
	}
	pubkey, err := ident.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("no atproto signing key found in %s DID document: %w", did, err)
	}

	car, err := r.getRecordCAR(ctx, pds, did, nsid, noCache)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Record %s: %w", uri, err)
	}

	commit, rep, err := repo.LoadRepoFromCAR(ctx, bytes.NewReader(car))
	if err != nil {
		return nil, fmt.Errorf("failed to verify Lexicon record proof at %s: %w", uri, err)
	}
	if commit.DID != did.String() {
		return nil, fmt.Errorf("failed to verify Lexicon record proof at %s: invalid repo did: %s", uri, commit.DID)
	}
	if err := commit.VerifySignature(pubkey); err != nil {
		return nil, fmt.Errorf("failed to verify Lexicon record proof at %s: %w", uri, err)
	}

	recordBytes, recordCID, err := rep.GetRecordBytes(ctx, lexiconCollection, syntax.RecordKey(nsid.String()))
	if err != nil {
		return nil, fmt.Errorf("failed to verify Lexicon record proof at %s: %w", uri, err)
	}

	decoded, err := decodeCBOR(recordBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid Lexicon document at %s: %w", uri, err)
	}
	doc, ok := decoded.(*orderedMap)
	if !ok {
		return nil, fmt.Errorf("invalid Lexicon document at %s: not a map", uri)
	}
	if typ, _ := doc.getString("$type"); typ != lexiconCollection {
		return nil, fmt.Errorf("failed to verify Lexicon record proof at %s: invalid record type: expected %s, got %q", uri, lexiconCollection, typ)
	}
	if err := validateLexiconDoc(doc); err != nil {
		return nil, fmt.Errorf("invalid Lexicon document at %s: %w", uri, err)
	}
	id, _ := doc.getString("id")
	if id != nsid.String() {
		return nil, fmt.Errorf("invalid document id %q at %s", id, uri)
	}

	return &resolvedLexicon{doc: doc, id: id, cid: recordCID.String()}, nil
}

// getRecordCAR fetches the com.atproto.sync.getRecord proof CAR from the PDS.
func (r *resolver) getRecordCAR(ctx context.Context, pds string, did syntax.DID, nsid syntax.NSID, noCache bool) ([]byte, error) {
	params := url.Values{}
	params.Set("did", did.String())
	params.Set("collection", lexiconCollection)
	params.Set("rkey", nsid.String())
	reqURL := fmt.Sprintf("%s/xrpc/com.atproto.sync.getRecord?%s", pds, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	if noCache {
		req.Header.Set("Cache-Control", "no-cache")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("com.atproto.sync.getRecord returned HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return io.ReadAll(resp.Body)
}

// validateLexiconDoc performs structural validation of a lexicon document.
// This is intentionally lighter than @atproto/lex-document's full schema
// validation: it checks the invariants install() itself relies on.
func validateLexiconDoc(doc *orderedMap) error {
	if lex, ok := doc.get("lexicon"); !ok {
		return fmt.Errorf("missing \"lexicon\" version field")
	} else if v, ok := lex.(int64); !ok || v != 1 {
		return fmt.Errorf("unsupported lexicon version: %v", lex)
	}
	id, ok := doc.getString("id")
	if !ok {
		return fmt.Errorf("missing \"id\" field")
	}
	if _, err := syntax.ParseNSID(id); err != nil {
		return fmt.Errorf("invalid \"id\" field: %w", err)
	}
	if _, ok := doc.getMap("defs"); !ok {
		return fmt.Errorf("missing \"defs\" field")
	}
	return nil
}
