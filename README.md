# glex

Go codegen for [AT Protocol](https://atproto.com) Lexicons. Vendor lexicon
schemas from the network, generate idiomatic Go types and XRPC clients, and
serialize them as canonical DAG-CBOR — the Go equivalent of Bluesky's
TypeScript [`@atproto/lex`] tool.

[`@atproto/lex`]: https://github.com/bluesky-social/atproto/tree/main/packages/lex

## Install

```sh
go install github.com/streamplace/glex/cmd/glex@latest
```

## Getting started

Vendor the lexicons your project uses. This resolves each NSID over the
network (with cryptographic proof verification), writes the schema documents
under `./lexicons/`, records CID-pinned resolutions in a `lexicons.json`
lockfile, and pulls in referenced lexicons recursively:

```sh
glex install app.bsky.feed.post
```

Then generate Go packages from them:

```sh
glex build --module-path github.com/you/yourproject/pkg --output-dir ./pkg
```

`glex build` re-runs the install from `lexicons.json` first (a no-op with no
network traffic when the vendored files are present), so a fresh clone of your
repo only ever needs `glex build`. Commit `lexicons/`, `lexicons.json`, and
the generated packages.

For `app.bsky.feed.post` this produces `pkg/appbsky/feedpost.go` with a `FeedPost`
struct, plus a package per referenced namespace. Using the generated types:

```go
import (
    glex "github.com/streamplace/glex/runtime"
    "github.com/you/yourproject/pkg/appbsky"
)

// Encode: $type is stamped automatically, for both JSON and CBOR.
post := &appbsky.FeedPost{Text: "hello", CreatedAt: "2024-01-01T00:00:00Z"}
b, err := json.Marshal(post)

// Decode when you know what the bytes must be — wrong $type is a hard error:
post, err := glex.CborDecodeAs[appbsky.FeedPost](recordBytes)

// Decode by $type when you don't (e.g. firehose records):
rec, err := glex.CborDecodeValue(recordBytes)
switch rec := rec.(type) {
case *appbsky.FeedPost:
    fmt.Println(rec.Text)
}
```

## CLI reference

### `glex build`

Generates Go source from the vendored lexicon JSON files.

| Flag | Default | Description |
|------|---------|-------------|
| `--lexicons-dir` | `lexicons/` | Directory containing lexicon JSON files (recursive) |
| `--output-dir` / `--out` | `./pkg/` | Base directory for generated packages |
| `--module-path` | | Go import path the generated packages live under (e.g. `github.com/you/yourproject/pkg`) |
| `--manifest` | `./lexicons.json` | Manifest used by the pre-build install |
| `--no-install` | off | Skip the pre-build install |
| `--gen-server` | | Also generate server handler stubs into the given package name |
| `--no-imports-tidy` | off | Skip goimports cleanup of the output |
| `--verbose` / `-v` | off | Print each generated lexicon |

### `glex install`

Vendors lexicon documents from the network and records them in
`lexicons.json`. Output is byte-for-byte compatible with `@atproto/lex`'s
`lex install`, so the two tools can be used interchangeably on the same
repository.

```sh
glex install app.bsky.feed.post   # install an NSID (and its dependencies)
glex install                      # re-install everything from lexicons.json
glex install --update             # re-fetch the latest version of everything
glex install --ci                 # lockfile check for CI
```

| Flag | Default | Description |
|------|---------|-------------|
| `--manifest` | `./lexicons.json` | Path to the manifest file |
| `--lexicons` | `./lexicons` | Directory lexicon JSON files are vendored into |
| `--save` / `-s` | on | Update lexicons.json after installing (`--no-save` to disable) |
| `--update` | off | Re-resolve and re-fetch all installed lexicons |
| `--ci` | off | Error if installed lexicons don't match the manifest CIDs |

Tip: generated files and the tool-formatted `lexicons.json` / `lexicons/`
directories are best excluded from your formatter (e.g. `.prettierignore`) —
the installers own their formatting.

## The runtime

Generated code depends on `github.com/streamplace/glex/runtime` (package
`glex`). The pieces you'll touch directly:

- **Typed decode** — `glex.DecodeCBOR(b, &rec)` / `glex.DecodeJSON(b, &rec)`,
  or the expression-shaped `glex.CborDecodeAs[T](b)` / `glex.JsonDecodeAs[T](b)`.
  The wire `$type` is verified against the target type; a mismatch is a hard
  error, never a silently zero-filled struct.
- **Decode by `$type`** — `glex.CborDecodeValue(b)` / `glex.JsonDecodeValue(b)`
  dispatch through the type registry and return a `glex.Record`. Only pointers
  to generated types satisfy `Record`, so a wrong value-type assertion is a
  compile error. `glex.RecordAs[T](rec)` converts a `Record` with the same
  hard-error semantics.
- **Value types** — `glex.Link` (CID link), `glex.Blob` (blob reference),
  `glex.Bytes` (byte string).
- **Unions** — generated as one struct per union with a pointer field per
  variant; exactly one should be set. An unrecognized variant decodes into the
  union's `Raw` field and re-encodes verbatim, so unknown data round-trips
  losslessly.
- **`unknown` fields** — generated as `*glex.LexiconTypeDecoder`. To put a
  non-lexicon payload (e.g. a DID document) in one: `glex.Unknown(v)`.
- **CIDs** — `glex.CidForRecord(rec)` computes the canonical record CID
  (stamping `$type` the same way marshaling does).

## Status

glex is pre-1.0 and under active development: the runtime, generator,
installer, and CLI are in production use, while subscription message types and
the standalone XRPC client are still in progress.

## License

MIT. See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and project
lineage.
