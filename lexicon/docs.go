/*
Package lexicon parses and validates AT Protocol Lexicon schema documents.

This package is derived from github.com/bluesky-social/indigo/atproto/lexicon
(MIT licensed; originally by the Bluesky Social PBC indigo authors), targeting
the permissioned-data (spaces) language from bluesky-social/proposals 0016.
glex vendors it so the parser used by the generator can accept `space`
definitions and `space` permissions without depending on an unreleased indigo
branch, and so downstream users can share the exact parser glex uses.

Upstream sources for this copy:

  - indigo bnewbold/spaces @ fc6469fc67c67541efae54c53e80298a12ed9ba0
  - bluesky-social/atproto permissioned-data @
    98ef3a6b9d1364f0543f137dd024de9651b21c4c
    (packages/lex/lex-document/src/lexicon-document.ts and the
    lexicons/com/atproto/{space,personalspace,memberspace,managedspace} tree)

Divergences from upstream indigo:

  - `space` schema definitions and the `space-ref` string format are accepted.
  - Permission definitions carry the space-specific parameters `spaceType`,
    `authority`, `skey`, and `manage`, and validate them.
  - The networked resolver (`ResolveLexiconData`, `ResolvingCatalog`) is not
    included; glex install has its own proof-verifying resolver.
*/
package lexicon
