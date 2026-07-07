// Package lexgen implements Go code generation from AT Protocol Lexicon schemas.
//
// It is a fork of indigo's lex/lexgen (by bnewbold, MIT), extended with a
// go-dasl (DRISL) codegen profile: generated structs serialize as canonical
// DAG-CBOR via go-dasl rather than whyrusleeping/cbor-gen, using thin adapter
// methods that delegate to the glex runtime.
package lexgen
