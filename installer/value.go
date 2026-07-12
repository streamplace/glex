package installer

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	daslcid "github.com/hyphacoop/go-dasl/cid"
	"github.com/ipfs/go-cid"
)

// orderedMap is a JSON/CBOR map that remembers key insertion order, mirroring
// how JavaScript objects preserve property order. Bitexact output parity with
// @atproto/lex depends on reproducing that order when re-serializing.
type orderedMap struct {
	keys   []string
	values map[string]any
}

func newOrderedMap() *orderedMap {
	return &orderedMap{values: map[string]any{}}
}

func (m *orderedMap) set(key string, value any) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

func (m *orderedMap) get(key string) (any, bool) {
	v, ok := m.values[key]
	return v, ok
}

func (m *orderedMap) getString(key string) (string, bool) {
	s, ok := m.values[key].(string)
	return s, ok
}

func (m *orderedMap) getMap(key string) (*orderedMap, bool) {
	v, ok := m.values[key].(*orderedMap)
	return v, ok
}

// cidLink is a decoded CBOR tag-42 link, kept in its string form.
type cidLink string

// decodeCBOR decodes a single DAG-CBOR value, preserving map key order as it
// appears in the encoded bytes (the same order @atproto/lex sees after cborg
// decoding, and therefore the order it writes JSON keys in).
func decodeCBOR(data []byte) (any, error) {
	d := &cborDecoder{buf: data}
	v, err := d.value()
	if err != nil {
		return nil, err
	}
	if d.pos != len(data) {
		return nil, fmt.Errorf("unexpected %d trailing bytes after CBOR value", len(data)-d.pos)
	}
	return v, nil
}

type cborDecoder struct {
	buf []byte
	pos int
}

func (d *cborDecoder) byte() (byte, error) {
	if d.pos >= len(d.buf) {
		return 0, fmt.Errorf("unexpected end of CBOR data")
	}
	b := d.buf[d.pos]
	d.pos++
	return b, nil
}

func (d *cborDecoder) take(n uint64) ([]byte, error) {
	if n > uint64(len(d.buf)-d.pos) {
		return nil, fmt.Errorf("unexpected end of CBOR data")
	}
	b := d.buf[d.pos : d.pos+int(n)]
	d.pos += int(n)
	return b, nil
}

// head reads a major type and its argument value.
func (d *cborDecoder) head() (major byte, arg uint64, err error) {
	ib, err := d.byte()
	if err != nil {
		return 0, 0, err
	}
	major = ib >> 5
	info := ib & 0x1f
	switch {
	case info < 24:
		arg = uint64(info)
	case info == 24:
		b, err := d.byte()
		if err != nil {
			return 0, 0, err
		}
		arg = uint64(b)
	case info == 25:
		b, err := d.take(2)
		if err != nil {
			return 0, 0, err
		}
		arg = uint64(binary.BigEndian.Uint16(b))
	case info == 26:
		b, err := d.take(4)
		if err != nil {
			return 0, 0, err
		}
		arg = uint64(binary.BigEndian.Uint32(b))
	case info == 27:
		b, err := d.take(8)
		if err != nil {
			return 0, 0, err
		}
		arg = binary.BigEndian.Uint64(b)
	default:
		return 0, 0, fmt.Errorf("unsupported CBOR additional info %d (indefinite lengths are not allowed)", info)
	}
	return major, arg, nil
}

func (d *cborDecoder) value() (any, error) {
	start := d.pos
	major, arg, err := d.head()
	if err != nil {
		return nil, err
	}
	switch major {
	case 0: // unsigned int
		if arg > math.MaxInt64 {
			return nil, fmt.Errorf("integer out of int64 range")
		}
		return int64(arg), nil
	case 1: // negative int
		if arg > math.MaxInt64 {
			return nil, fmt.Errorf("integer out of int64 range")
		}
		return -1 - int64(arg), nil
	case 2: // byte string
		b, err := d.take(arg)
		if err != nil {
			return nil, err
		}
		out := make([]byte, len(b))
		copy(out, b)
		return out, nil
	case 3: // text string
		b, err := d.take(arg)
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(b) {
			return nil, fmt.Errorf("invalid UTF-8 in CBOR text string")
		}
		return string(b), nil
	case 4: // array
		arr := make([]any, 0, arg)
		for i := uint64(0); i < arg; i++ {
			v, err := d.value()
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	case 5: // map
		m := newOrderedMap()
		for i := uint64(0); i < arg; i++ {
			k, err := d.value()
			if err != nil {
				return nil, err
			}
			key, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("non-string CBOR map key")
			}
			if _, dup := m.get(key); dup {
				return nil, fmt.Errorf("duplicate CBOR map key %q", key)
			}
			v, err := d.value()
			if err != nil {
				return nil, err
			}
			m.set(key, v)
		}
		return m, nil
	case 6: // tag
		if arg != 42 {
			return nil, fmt.Errorf("unsupported CBOR tag %d", arg)
		}
		inner, err := d.value()
		if err != nil {
			return nil, err
		}
		b, ok := inner.([]byte)
		if !ok || len(b) == 0 || b[0] != 0 {
			return nil, fmt.Errorf("invalid CID in CBOR tag 42: expected byte string with leading 0x00 (offset %d)", start)
		}
		c, err := cid.Cast(b[1:])
		if err != nil {
			return nil, fmt.Errorf("invalid CID in CBOR tag 42: %w", err)
		}
		return cidLink(c.String()), nil
	case 7: // simple / float
		switch {
		case arg == 20:
			return false, nil
		case arg == 21:
			return true, nil
		case arg == 22:
			return nil, nil
		default:
			return nil, fmt.Errorf("unsupported CBOR simple value or float (major 7, arg %d)", arg)
		}
	}
	return nil, fmt.Errorf("unhandled CBOR major type %d", major)
}

// parseJSON parses JSON into the same ordered representation used for CBOR
// values: *orderedMap for objects, []any for arrays, int64 for integer
// numbers, plus string/bool/nil/float64.
func parseJSON(data []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	v, err := parseJSONValue(dec)
	if err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("unexpected trailing JSON data")
	}
	return v, nil
}

func parseJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return parseJSONToken(dec, tok)
}

func parseJSONToken(dec *json.Decoder, tok json.Token) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			m := newOrderedMap()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("unexpected JSON object key token %v", keyTok)
				}
				v, err := parseJSONValue(dec)
				if err != nil {
					return nil, err
				}
				m.set(key, v)
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return m, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := parseJSONValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("unexpected JSON delimiter %v", t)
	case json.Number:
		if i, err := strconv.ParseInt(t.String(), 10, 64); err == nil {
			return i, nil
		}
		f, err := t.Float64()
		if err != nil {
			return nil, err
		}
		return f, nil
	default:
		return t, nil // string, bool, nil
	}
}

// stringifyJSON serializes a value the way JavaScript's
// JSON.stringify(value, null, 2) does: two-space indent, object keys in
// insertion order, no trailing newline. []byte and cidLink values render in
// their @atproto/lex-json forms ({"$bytes": ...} / {"$link": ...}).
func stringifyJSON(v any) string {
	var sb strings.Builder
	writeJSONValue(&sb, v, "")
	return sb.String()
}

func writeJSONValue(sb *strings.Builder, v any, indent string) {
	switch t := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		if t {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case int64:
		sb.WriteString(strconv.FormatInt(t, 10))
	case float64:
		sb.WriteString(formatJSNumber(t))
	case string:
		writeJSONString(sb, t)
	case []byte:
		// lexToJson: Uint8Array → {"$bytes": <unpadded base64>}
		inner := newOrderedMap()
		inner.set("$bytes", base64.RawStdEncoding.EncodeToString(t))
		writeJSONValue(sb, inner, indent)
	case cidLink:
		inner := newOrderedMap()
		inner.set("$link", string(t))
		writeJSONValue(sb, inner, indent)
	case []any:
		if len(t) == 0 {
			sb.WriteString("[]")
			return
		}
		childIndent := indent + "  "
		sb.WriteString("[\n")
		for i, item := range t {
			sb.WriteString(childIndent)
			writeJSONValue(sb, item, childIndent)
			if i < len(t)-1 {
				sb.WriteByte(',')
			}
			sb.WriteByte('\n')
		}
		sb.WriteString(indent)
		sb.WriteByte(']')
	case *orderedMap:
		if len(t.keys) == 0 {
			sb.WriteString("{}")
			return
		}
		childIndent := indent + "  "
		sb.WriteString("{\n")
		for i, key := range t.keys {
			sb.WriteString(childIndent)
			writeJSONString(sb, key)
			sb.WriteString(": ")
			writeJSONValue(sb, t.values[key], childIndent)
			if i < len(t.keys)-1 {
				sb.WriteByte(',')
			}
			sb.WriteByte('\n')
		}
		sb.WriteString(indent)
		sb.WriteByte('}')
	default:
		panic(fmt.Sprintf("stringifyJSON: unhandled type %T", v))
	}
}

// writeJSONString escapes a string exactly like JavaScript's JSON.stringify:
// short escapes for the common control characters, lowercase \u00xx for the
// rest, and everything else (including non-ASCII) written literally.
func writeJSONString(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\b':
			sb.WriteString(`\b`)
		case '\t':
			sb.WriteString(`\t`)
		case '\n':
			sb.WriteString(`\n`)
		case '\f':
			sb.WriteString(`\f`)
		case '\r':
			sb.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(sb, `\u%04x`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
}

// formatJSNumber formats a float64 the way JavaScript Number#toString does
// for the ranges JSON.stringify emits without exponents. Lexicon documents
// contain only integers in practice (the AT data model forbids floats), so
// this exists for completeness.
func formatJSNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	// Go writes "1e+21"/"1e-07"; JS writes "1e+21"/"1e-7".
	if i := strings.Index(s, "e"); i >= 0 {
		mant, exp := s[:i], s[i+1:]
		exp = strings.Replace(exp, "+0", "+", 1)
		exp = strings.Replace(exp, "-0", "-", 1)
		s = mant + "e" + exp
	}
	return s
}

// toPlainValue converts an ordered value tree into plain Go values that
// drisl.Marshal understands, for CID computation over locally stored lexicon
// JSON files (the equivalent of @atproto/lex's cidForLex over a parsed file).
// Mirroring lex-cbor's encoder, non-integer numbers are rejected.
func toPlainValue(v any) (any, error) {
	switch t := v.(type) {
	case nil, bool, int64, string, []byte:
		return t, nil
	case float64:
		return nil, fmt.Errorf("non-integer numbers (%v) are not supported by the AT data model", t)
	case cidLink:
		c, err := daslcid.NewCidFromString(string(t))
		if err != nil {
			return nil, err
		}
		return c, nil
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			p, err := toPlainValue(item)
			if err != nil {
				return nil, err
			}
			out[i] = p
		}
		return out, nil
	case *orderedMap:
		out := make(map[string]any, len(t.keys))
		for _, key := range t.keys {
			p, err := toPlainValue(t.values[key])
			if err != nil {
				return nil, err
			}
			out[key] = p
		}
		return out, nil
	default:
		return nil, fmt.Errorf("toPlainValue: unhandled type %T", v)
	}
}
