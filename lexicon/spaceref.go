package lexicon

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// spaceRefRegex matches the `space-ref` format from proposal 0016:
//
//	at://{spaceDid}/space/{spaceType}/{skey}
//
// The authority, type, and key are re-validated with the atproto syntax
// parsers below.
var spaceRefRegex = regexp.MustCompile(`^at://(?P<authority>[a-zA-Z0-9._:%-]+)/space/(?P<stype>[a-zA-Z0-9.-]+)/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})$`)

// validateSpaceRef checks a `space-ref` string. It is intentionally local to
// this package: indigo's syntax.SpaceRef only exists on an unreleased branch,
// and glex must compile against released indigo versions.
func validateSpaceRef(raw string) error {
	if len(raw) > 8192 {
		return errors.New("space ref is too long (8192 chars max)")
	}
	parts := spaceRefRegex.FindStringSubmatch(raw)
	if parts == nil || parts[0] == "" {
		return errors.New("space ref syntax didn't validate via regex")
	}
	if _, err := syntax.ParseDID(parts[1]); err != nil {
		return fmt.Errorf("space ref authority section not a valid DID: %s", parts[1])
	}
	if _, err := syntax.ParseNSID(parts[2]); err != nil {
		return fmt.Errorf("space ref second path segment not an NSID: %s", parts[2])
	}
	if _, err := syntax.ParseRecordKey(parts[3]); err != nil {
		return fmt.Errorf("space ref third path segment not a space key (RecordKey): %s", parts[3])
	}
	return nil
}
