# glex — standalone atproto Go lexicon code generator

> A Go equivalent of Bluesky's TypeScript `@atproto/lex` tool: read Lexicon
> schemas, emit idiomatic Go types + XRPC clients, serialize as canonical
> DAG-CBOR via [go-dasl](https://github.com/hyphacoop/go-dasl) — **no
> `cbor_gen.go`, no `sed`, no bootstrap cycle.**

This file is a handoff/roadmap. The working prototype of everything below
already exists, but **inside Streamplace's infrastructure**, deliberately
coupled to indigo to keep that migration small. "Getting to glex" is mostly a
**decoupling + generalization** job, not new research.

---

## Where the prototype lives today

The generator and runtime were built during Streamplace's Go-codegen migration
(streamplace PR #1181). Two places:

1. **Generator** — a vendored, modified copy of indigo's `lex/lexgen` living in
   the Streamplace cobalt fork:
   - repo: `github.com/streamplace/cobalt`, branch `eli/go-dasl-codegen`
   - `lex/lexgen/codegen.go` — added `StreamplaceConfig()`, a `DaslMode` flag,
     `PkgNameOverrides`, `ExtraImports`, and `writeCBORAdapter()` (emits the
     `cbg.CBORMarshaler`-shaped adapters that delegate to go-dasl).
   - `cmd/glot/lex_codegen.go` — the `glot codegen --streamplace` entry point.
   - Invoked from streamplace's Makefile via
     `GOTOOLCHAIN=auto go run github.com/streamplace/cobalt/cmd/glot@<sha> codegen --streamplace ...`.

2. **Runtime support** — `stream.place/streamplace/pkg/lex` in the streamplace
   repo:
   - `link.go` / `blob.go` / `bytes.go` — go-dasl-native `Link`/`Blob`/`Bytes`
     value types, **byte-identical** DAG-CBOR to indigo's `lexutil` equivalents
     (verified; stable record CIDs).
   - `marshal.go` — `MarshalCBOR(io.Writer,v)` / `UnmarshalCBOR(io.Reader,v)`
     adapter helpers wrapping `drisl.Marshal/Unmarshal`.
   - `bridge.go` — `BlobFromLexUtil` / `(*Blob).LexUtil()` conversions to
     indigo's `lexutil.LexBlob` at library boundaries.

Read those files first — they are the seed of glex.

---

## The core idea (what makes this better than what exists)

- **go-dasl does CBOR by runtime reflection.** Generated structs just need
  correct struct tags; there is *no* per-type marshal codegen (contrast
  indigo's `whyrusleeping/cbor-gen`, which emits ~13k-line files and needs a
  stub/bootstrap dance for union variants). This is the killer feature.
- **The cbg-adapter trick**: each generated struct implements
  `MarshalCBOR(io.Writer)/UnmarshalCBOR(io.Reader)` (indigo's `cbg` interface)
  by delegating to go-dasl. This lets generated types drop straight into
  indigo's repo/carstore/MST plumbing *and* the `lexutil` `$type` registry
  while serializing through go-dasl. Output is byte-for-byte identical to
  cbor-gen, so CIDs are stable — a migration is safe.
- Unions keep a small hand-emitted `$type` dispatch (go-dasl has no union
  machinery); records/objects need nothing but tags + the adapter.

Be honest in the README about lineage: the heavy lifting is **go-dasl's**
(canonical DAG-CBOR by reflection) and the generator skeleton is **indigo's
`lex/lexgen`** (bnewbold, MIT). glex's value-add is the go-dasl profile, the
adapter pattern, the self-contained runtime, and packaging it as a real tool.

---

## Work to reach a standalone tool

Roughly in dependency order. Steps 1–2 are the bulk and are the *same work* as
cleanly finishing the Streamplace migration — not throwaway.

### 1. Complete the runtime library (the biggest piece)

`pkg/lex` today only owns `Link`/`Blob`/`Bytes` + adapters; everything else is
still indigo's `lexutil`. glex needs a self-contained runtime (call it
`glex/runtime` or a `glex` module) reimplementing, on top of go-dasl:

- The `$type` → Go-type **registry** + `RegisterType` (replaces
  `lexutil.RegisterType` + indigo's global `lexTypesMap`).
- `CborDecodeValue([]byte) (any, error)` — decode-by-`$type` (the firehose
  workhorse) + an `ErrUnrecognizedType` sentinel.
- A `LexiconTypeDecoder` equivalent (the open "unknown record" wrapper used in
  view types) — holds a decoded value, marshals with `$type`.
- `TypeExtract` (JSON) / `CborTypeExtractReader` (CBOR) for union dispatch.
- An XRPC `Client` interface (`LexClient`/`LexDo` equivalent) + `Query`/
  `Procedure` constants, so generated endpoint funcs have something to call.

The full surface was characterized during the Streamplace work; reimplementing
on go-dasl is tractable. **Drop the `bridge.go` dependency on indigo** here.

### 2. Make the generator config-driven (de-Streamplace-ify)

The prototype hardcodes Streamplace specifics that must become configuration:

- `LexImport` — hardcoded `stream.place/streamplace/pkg/lex`. → configurable
  runtime import path.
- `PkgNameOverrides` — `placestream→streamplace`, 5×`games`→4×`games`. →
  general NSID-authority → Go-package-name map.
- `ExtraImports` — streamplace's cross-package refs. → general
  "authority → (package alias, import path)" mapping.
- External type mappings — currently hardcoded to indigo's `api/atproto`,
  `api/bsky`, `api/chat`, `api/ozone` (see `deps()`). → configurable, or
  removed entirely once base types are generated (step 3).

Design a small **build-file** format (a modernized version of indigo's old
`--build-file lexgen-types.json`): input lexicon dirs, output dir, per-authority
package/import mapping, runtime import path, and external mappings. This is the
main API-design task.

Also decide whether to keep `LegacyMode` (cborgen tags, package remaps,
`RegisterType` inits) or move to a clean go-dasl-native profile:
- go-dasl reads `json:` tags, so the `cborgen:` tags become vestigial (they're
  currently only still consumed by indigo's `LexiconTypeDecoder.MarshalJSON`
  `const=` lookup — which the new runtime can drop).
- Prefer a single clean profile; keep a `--legacy`/indigo-compat mode only if a
  migration path needs it.

### 3. Generate the base namespaces locally (flip to fully standalone)

Today external refs (`com.atproto.*`, `app.bsky.*`) resolve to **indigo's**
generated types. A standalone tool should instead **generate them itself** from
the canonical lexicon JSON (Streamplace already pulls these via `@atproto/lex`;
the committed `lexicons/` tree + upstream atproto lexicons are the source).

Key facts established during the Streamplace work:
- This is **barely any extra generator code** — just more lexicons through the
  same pipeline.
- It does **not** break the wire format or indigo's repo/carstore/MST/events
  plumbing (those are type-agnostic: bytes + `cbg.CBORMarshaler`).
- What it changes is **Go type identity**: glex-generated `com.atproto.*` types
  are distinct from indigo's, so any code that calls indigo's *library
  convenience* APIs (XRPC helpers, `PutRecord` inputs, `EmbedExternal`, the
  firehose decode targets) needs thin conversion bridges — the same pattern as
  `pkg/lex`'s blob bridge, just broader. The repo/MST/firehose *plumbing* keeps
  working untouched.
- **Bonus**: sourcing base lexicons from committed JSON removes the
  `go list -m github.com/bluesky-social/indigo` / module-cache lexicon
  dependency (which caused a real GitLab-CI failure — cold module cache →
  empty dir). Committed JSON is strictly more reproducible.

### 4. Round out the emitted surface

- Subscription / event message types.
- A real, runtime-backed **XRPC client** generator (the prototype emits endpoint
  wrapper funcs but they're indigo-`lexutil.LexClient`-shaped and unused by
  Streamplace). Optionally a server-handler generator.
- Revisit `map[string]any` vs a typed decoder for `unknown`/open unions.

### 5. Package it as a tool

- Own module `github.com/streamplace/glex`, a `glex` CLI (`glex gen`,
  probably reusing the `atproto/lexicon` parser + `lexgen.FlattenSchemaFile`
  flattener from indigo, which are stable and unopinionated).
- Golden-file tests: fixture lexicons → expected `.go` output; plus a
  round-trip + CID-stability suite (marshal → decode-by-`$type` → concrete
  type → re-marshal byte-stable). The Streamplace `pkg/lex` tests are a
  starting point.
- Docs, versioning, `CONTRIBUTING`.

---

## Real considerations / eyes-open (not hard blockers)

- **Supply chain**: go-dasl is `v0.8.0` (pre-1.0) and its `hyphacoop/cbor` fork
  is an *unreleased pseudo-version*. Publishing on top means betting on those
  stabilizing (or vendoring). Worth a conversation with hyphacoop.
- **Go 1.26 / `GOTOOLCHAIN`**: indigo's lexgen pulls in a `go 1.26` requirement;
  containers pinned to older Go need `GOTOOLCHAIN=auto` (this bit Streamplace's
  CI). A published tool inherits that adoption papercut — consider minimizing
  the indigo dependency in the generator itself (only `atproto/lexicon` +
  `atproto/syntax` are truly needed for parse/flatten).
- **Upstream vs fork**: `lex/lexgen` and `glot` are bnewbold's active WIP. There
  is a real choice between shipping glex as an independent tool and contributing
  the go-dasl profile upstream to `glot`. Upstreaming is better for the
  ecosystem; a standalone tool gives more control and a cleaner runtime story.
  At minimum, float it with bnewbold / the hyphacoop folks.

## The end-state architecture (for a consumer like Streamplace)

Not "no indigo" — indigo's repo/carstore/events/identity/atcrypto plumbing is a
huge amount of battle-tested code you don't want to reimplement. The clean shape
is:

> **glex-generated types + glex runtime for the data model; indigo for the
> plumbing; thin conversion bridges between them.**

That's the same architecture whether you're doing it just for Streamplace or
shipping glex for everyone — which is why finishing the Streamplace decoupling
*is* building glex.
