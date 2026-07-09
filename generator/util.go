package lexgen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

func defType(sd *lexicon.SchemaDef) (string, error) {
	switch sd.Inner.(type) {
	case lexicon.SchemaRecord:
		return "record", nil
	case lexicon.SchemaQuery:
		return "query", nil
	case lexicon.SchemaProcedure:
		return "procedure", nil
	case lexicon.SchemaSubscription:
		return "subscription", nil
	case lexicon.SchemaPermissionSet:
		return "permission-set", nil
	case lexicon.SchemaPermission:
		return "permission", nil
	case lexicon.SchemaBoolean:
		return "boolean", nil
	case lexicon.SchemaInteger:
		return "integer", nil
	case lexicon.SchemaString:
		return "string", nil
	case lexicon.SchemaBytes:
		return "bytes", nil
	case lexicon.SchemaCIDLink:
		return "cid-link", nil
	case lexicon.SchemaArray:
		return "array", nil
	case lexicon.SchemaObject:
		return "object", nil
	case lexicon.SchemaBlob:
		return "blob", nil
	case lexicon.SchemaParams:
		return "params", nil
	case lexicon.SchemaToken:
		return "token", nil
	case lexicon.SchemaRef:
		return "ref", nil
	case lexicon.SchemaUnion:
		return "union", nil
	case lexicon.SchemaUnknown:
		return "unknown", nil
	default:
		return "", fmt.Errorf("unhandled schema type: %T", sd.Inner)
	}
}

func defDescription(sd *lexicon.SchemaDef) string {
	var desc *string

	switch v := sd.Inner.(type) {
	case lexicon.SchemaRecord:
		desc = v.Description
	case lexicon.SchemaQuery:
		desc = v.Description
	case lexicon.SchemaProcedure:
		desc = v.Description
	case lexicon.SchemaSubscription:
		desc = v.Description
	case lexicon.SchemaPermissionSet:
		desc = v.Description
	case lexicon.SchemaPermission:
		desc = v.Description
	case lexicon.SchemaBoolean:
		desc = v.Description
	case lexicon.SchemaInteger:
		desc = v.Description
	case lexicon.SchemaString:
		desc = v.Description
	case lexicon.SchemaBytes:
		desc = v.Description
	case lexicon.SchemaCIDLink:
		desc = v.Description
	case lexicon.SchemaArray:
		desc = v.Description
	case lexicon.SchemaObject:
		desc = v.Description
	case lexicon.SchemaBlob:
		desc = v.Description
	case lexicon.SchemaParams:
		desc = v.Description
	case lexicon.SchemaToken:
		desc = v.Description
	case lexicon.SchemaRef:
		desc = v.Description
	case lexicon.SchemaUnion:
		desc = v.Description
	case lexicon.SchemaUnknown:
		desc = v.Description
	}
	if desc != nil && *desc != "" {
		return *desc
	}
	return ""
}

func isCompoundDef(sd *lexicon.SchemaDef) bool {
	switch sd.Inner.(type) {
	case lexicon.SchemaRecord, lexicon.SchemaQuery, lexicon.SchemaProcedure, lexicon.SchemaSubscription, lexicon.SchemaArray, lexicon.SchemaObject, lexicon.SchemaUnion:
		return true
	default:
		return false
	}
}

// nsidPkgName computes the Go package name for an NSID. The NSID authority is
// a reversed domain (e.g. "repo.atproto.com" for "com.atproto.repo"). The
// last two labels of the authority are the registered domain (e.g.
// "atproto.com"); reversing them gives the package name (e.g. "comatproto").
func nsidPkgName(nsid syntax.NSID) string {
	auth := strings.ToLower(nsid.Authority())
	parts := strings.Split(auth, ".")
	regParts := parts[len(parts)-2:]
	slices.Reverse(regParts)
	return strings.Join(regParts, "")
}

// nsidBaseName computes the Go type name for an NSID. It takes the subdomain
// labels (everything before the registered domain), reverses them, appends
// the NSID name, and Title-cases each part. E.g. "com.atproto.repo.strongRef"
// → authority "repo.atproto.com" → subdomain ["repo"] → "RepoStrongRef".
func nsidBaseName(nsid syntax.NSID) string {
	auth := strings.ToLower(nsid.Authority())
	parts := strings.Split(auth, ".")
	subParts := parts[:len(parts)-2]
	slices.Reverse(subParts)
	subParts = append(subParts, nsid.Name())
	for i := range subParts {
		subParts[i] = strings.Title(subParts[i])
	}
	return strings.Join(subParts, "")
}

// nsidFileName computes the output filename for an NSID (lowercase, no
// extension). Same as nsidBaseName but all-lowercase.
func nsidFileName(nsid syntax.NSID) string {
	auth := strings.ToLower(nsid.Authority())
	parts := strings.Split(auth, ".")
	subParts := parts[:len(parts)-2]
	slices.Reverse(subParts)
	subParts = append(subParts, strings.ToLower(nsid.Name()))
	return strings.Join(subParts, "")
}
