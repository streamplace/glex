# Contributing to glex

## Development setup

```sh
git clone https://github.com/streamplace/glex
cd glex
go build ./cmd/glex
go test ./...
```

Go 1.24+ is required (go-dasl dependency). Set `GOTOOLCHAIN=auto` if your
system Go is older.

## Architecture overview

glex has three components:

1. **`runtime/`** (package `glex`) — the runtime support library. Value
   types (Link, Blob, Bytes), adapter helpers, `$type` registry, decode
   machinery, XRPC client interface. Depends on go-dasl for CBOR and
   whyrusleeping/cbor-gen only for low-level CID/byte-string primitives.

2. **`generator/`** (package `lexgen`) — the code generator. A fork of
   indigo's `lex/lexgen` with a go-dasl profile. Reads lexicon JSON, emits Go
   source. Configurable via `GenConfig`.

3. **`cmd/glex/`** — the CLI. `glex build` reads lexicons and writes `.go`
   files.

## Testing changes

After modifying the generator, always run:

```sh
# Regenerate golden files if output format changed
go test ./cmd/glex/ -update

# Verify golden test passes
go test ./cmd/glex/

# Regenerate test fixtures and run round-trip tests
go run ./cmd/glex build --lexicons-dir testdata/lexicons --output-dir testdata/gentest
go test ./testdata/gentest/

# Full suite
go test ./...
```

## Adding fixture lexicons

Add new `.json` files under `testdata/lexicons/`. Follow the AT Protocol
convention: each file has one lexicon with a `main` definition. Run with
`-update` to generate golden files for the new fixtures.

## Lineage and licensing

The generator skeleton (`flatten.go`, `util.go`, the codegen structure) is
derived from indigo's `lex/lexgen` by bnewbold (MIT). The CBOR serialization is
go-dasl's (hyphacoop). glex's contribution is the go-dasl profile, the adapter
pattern, the self-contained runtime, and the CLI packaging.
