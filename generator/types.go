package lexgen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

func (gen *CodeGenerator) fieldType(fname string, def *lexicon.SchemaDef, optional bool) (string, error) {
	// NOTE: SchemaObject and SchemaUnion should be handled outside this function; as well as arrays of those types also count
	// TODO: another pass to check for type completeness
	switch v := def.Inner.(type) {
	case lexicon.SchemaBoolean:
		if optional {
			return "*bool", nil
		}
		return "bool", nil
	case lexicon.SchemaInteger:
		if optional {
			return "*int64", nil
		}
		return "int64", nil
	case lexicon.SchemaString:
		if optional {
			return "*string", nil
		}
		return "string", nil
	case lexicon.SchemaBytes:
		// NOTE: not using a pointer for optional
		return gen.rtAlias() + ".Bytes", nil
	case lexicon.SchemaCIDLink:
		linkType := gen.rtAlias() + ".Link"
		if optional {
			return "*" + linkType, nil
		}
		return linkType, nil
	case lexicon.SchemaBlob:
		blobType := gen.rtAlias() + ".Blob"
		if optional {
			return "*" + blobType, nil
		}
		return blobType, nil
	case lexicon.SchemaArray:
		t, err := gen.fieldType(fname, &v.Items, false)
		if err != nil {
			return "", err
		}
		// NOTE: not using a pointer for optional
		return "[]" + t, nil
	case lexicon.SchemaUnknown:
		return "*" + gen.regAlias() + ".LexiconTypeDecoder", nil
	case lexicon.SchemaRef:
		ptr := ""
		if optional {
			ptr = "*"
		}

		// check for local references to concrete types first
		if strings.HasPrefix(v.Ref, "#") {
			dt, ok := gen.Lex.Defs[v.Ref[1:]]
			if !ok {
				return "", fmt.Errorf("broken self-reference: %s", v.Ref)
			}
			switch dt.Type {
			case "string":
				return ptr + "string", nil
			case "integer":
				return ptr + "int64", nil
			case "boolean":
				return ptr + "bool", nil
			// TODO: "unknown", "ref", "token", etc
			case "array":
				// TODO: more completeness here (eg, non-object types)
				return fmt.Sprintf("[]%s_%s_Elem", gen.baseName(), strings.Title(v.Ref[1:])), nil
			default: // presumed "object", "union"
				if v.Ref == "#main" {
					return ptr + gen.baseName(), nil
				}
				return fmt.Sprintf("%s%s_%s", ptr, gen.baseName(), strings.Title(v.Ref[1:])), nil
			}
		}

		// external reference
		t, err := gen.externalRefType(v.Ref)
		if err != nil {
			return "", err
		}
		return ptr + t, nil
	default:
		return "", fmt.Errorf("unhandled schema type in struct field: %T", def.Inner)
	}
}

func (gen *CodeGenerator) externalRefType(ref string) (string, error) {
	s, err := gen.Cat.Resolve(ref)
	if err != nil {
		return "", fmt.Errorf("could not resolve lexicon reference (%s): %w", ref, err)
	}

	switch s.Def.(type) {
	case lexicon.SchemaString:
		return "string", nil
		// TODO: other concrete types and special-cases types, like arrays
	}

	parts := strings.SplitN(ref, "#", 3)
	if len(parts) > 2 {
		return "", fmt.Errorf("failed to parse external ref: %s", ref)
	}
	nsid, err := syntax.ParseNSID(parts[0])
	if err != nil {
		return "", fmt.Errorf("failed to parse external ref NSID (%s): %w", ref, err)
	}

	// check if this is actually in the same package (which might not mean the same NSID authority)
	if gen.pkgNameFor(nsid) == gen.pkgNameFor(gen.Lex.NSID) {
		if len(parts) == 1 || parts[1] == "main" {
			return nsidBaseName(nsid), nil
		}
		return fmt.Sprintf("%s_%s", nsidBaseName(nsid), strings.Title(parts[1])), nil
	}

	if len(parts) == 1 || parts[1] == "main" {
		return fmt.Sprintf("%s.%s", nsidPkgName(nsid), nsidBaseName(nsid)), nil
	}
	return fmt.Sprintf("%s.%s_%s", nsidPkgName(nsid), nsidBaseName(nsid), strings.Title(parts[1])), nil
}

func (gen *CodeGenerator) writeStruct(ft *FlatType, obj *lexicon.SchemaObject) error {

	name := gen.baseName()
	if ft.DefName != "main" {
		name += "_" + strings.Title(ft.DefName)
	}
	for _, sub := range ft.Path {
		name += "_" + strings.Title(sub)
	}

	if ft.DefName != "main" && len(ft.Path) == 0 {
		fmt.Fprintf(gen.Out, "// %s is a \"%s\" in the %s schema.\n", name, ft.DefName, gen.Lex.NSID)
		if obj.Description != nil {
			fmt.Fprintln(gen.Out, "//")
			for _, l := range strings.Split(*obj.Description, "\n") {
				fmt.Fprintf(gen.Out, "// %s\n", l)
			}
		}
	} else if obj.Description != nil {
		for _, l := range strings.Split(*obj.Description, "\n") {
			fmt.Fprintf(gen.Out, "// %s\n", l)
		}
	}

	hasType := false
	typeConst := ""
	fmt.Fprintf(gen.Out, "type %s struct {\n", name)

	// emit $type field. omitempty so an unset $type is absent from output
	// rather than an invalid `"$type": ""` on the wire.
	if _, ok := obj.Properties["$type"]; !ok {
		hasType = true
		typeConst = gen.Lex.NSID.String()
		if ft.DefName != "main" {
			typeConst = gen.Lex.NSID.String() + "#" + ft.DefName
		}
		fmt.Fprintf(gen.Out, "\tLexiconTypeID string `json:\"$type,omitempty\"`\n")
	}

	// iterate in sorted order for deterministic output
	keys := make([]string, 0, len(obj.Properties))
	for k := range obj.Properties {
		if k == "$type" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, fname := range keys {
		field := obj.Properties[fname]
		optional := !isRequired(obj.Required, fname)

		// Handle object/union fields and arrays of them directly, since
		// fieldType doesn't have the path context to compute the right name.
		// This matches the original lexgen behavior.
		var t string
		switch v := field.Inner.(type) {
		case lexicon.SchemaObject, lexicon.SchemaUnion:
			t = name + "_" + strings.Title(fname)
			if optional {
				t = "*" + t
			}
		case lexicon.SchemaArray:
			switch v.Items.Inner.(type) {
			case lexicon.SchemaObject, lexicon.SchemaUnion:
				t = fmt.Sprintf("[]%s_%s_Elem", name, strings.Title(fname))
			default:
				var err error
				t, err = gen.fieldType(fname, &field, optional)
				if err != nil {
					return err
				}
			}
		default:
			var err error
			t, err = gen.fieldType(fname, &field, optional)
			if err != nil {
				return err
			}
		}

		omitempty := ""
		if optional {
			omitempty = ",omitempty"
		}

		desc := defDescription(&field)
		if desc != "" {
			fmt.Fprintf(gen.Out, "\t// %s: %s\n", fname, desc)
		}
		fmt.Fprintf(gen.Out, "\t%s %s", strings.ReplaceAll(strings.Title(fname), "-", ""), t)
		fmt.Fprintf(gen.Out, " `json:\"%s%s\"`\n", fname, omitempty)
	}
	fmt.Fprintf(gen.Out, "}\n\n")

	// Records and standalone main objects are the $type-mandatory,
	// registry-registered types; stamp $type on their JSON output the same
	// way the CBOR adapter does, so callers can't emit one without it.
	stampJSON := false
	switch ft.Schema.Inner.(type) {
	case lexicon.SchemaRecord:
		stampJSON = true
	case lexicon.SchemaObject:
		stampJSON = ft.DefName == "main" && len(ft.Path) == 0
	}

	gen.writeCBORAdapter(name, hasType, typeConst, stampJSON)

	return nil
}

// writeCBORAdapter emits cbg.CBORMarshalor-shaped methods that delegate to
// go-dasl (via the runtime), so generated structs interoperate with indigo's
// repo/carstore/MST layer while serializing as canonical DAG-CBOR through
// go-dasl. It also emits the pointer-receiver RecordTypeID method that makes
// *T (and only *T, never a bare T) satisfy the runtime's sealed Record
// interface, and — for $type-mandatory types (stampJSON) — a MarshalJSON
// that stamps $type the same way MarshalCBOR does.
func (gen *CodeGenerator) writeCBORAdapter(name string, hasType bool, typeConst string, stampJSON bool) {
	rt := gen.rtAlias()
	if hasType {
		fmt.Fprintf(gen.Out, "// RecordTypeID implements %s.Record.\n", rt)
		fmt.Fprintf(gen.Out, "func (t *%s) RecordTypeID() string { return %q }\n\n", name, typeConst)
	}
	if hasType && stampJSON {
		fmt.Fprintf(gen.Out, "// MarshalJSON stamps the $type field, like MarshalCBOR does. The value\n")
		fmt.Fprintf(gen.Out, "// receiver operates on a copy, so the record is never mutated and both\n")
		fmt.Fprintf(gen.Out, "// %s and *%s marshal with $type.\n", name, name)
		fmt.Fprintf(gen.Out, "func (t %s) MarshalJSON() ([]byte, error) {\n", name)
		fmt.Fprintf(gen.Out, "\tt.LexiconTypeID = %q\n", typeConst)
		fmt.Fprintf(gen.Out, "\ttype alias %s\n", name)
		fmt.Fprintf(gen.Out, "\treturn json.Marshal((alias)(t))\n")
		fmt.Fprintf(gen.Out, "}\n\n")
	}
	fmt.Fprintf(gen.Out, "func (t *%s) MarshalCBOR(w io.Writer) error {\n", name)
	fmt.Fprintf(gen.Out, "\tif t == nil {\n")
	fmt.Fprintf(gen.Out, "\t\t_, err := w.Write(cbg.CborNull)\n")
	fmt.Fprintf(gen.Out, "\t\treturn err\n")
	fmt.Fprintf(gen.Out, "\t}\n")
	if hasType {
		fmt.Fprintf(gen.Out, "\t// stamp $type on a copy so marshal never mutates the record\n")
		fmt.Fprintf(gen.Out, "\tcp := *t\n")
		fmt.Fprintf(gen.Out, "\tcp.LexiconTypeID = %q\n", typeConst)
		fmt.Fprintf(gen.Out, "\treturn %s.MarshalCBOR(w, &cp)\n", rt)
	} else {
		fmt.Fprintf(gen.Out, "\treturn %s.MarshalCBOR(w, t)\n", rt)
	}
	fmt.Fprintf(gen.Out, "}\n\n")

	fmt.Fprintf(gen.Out, "func (t *%s) UnmarshalCBOR(r io.Reader) error {\n", name)
	fmt.Fprintf(gen.Out, "\treturn %s.UnmarshalCBOR(r, t)\n", rt)
	fmt.Fprintf(gen.Out, "}\n\n")
}

type unionRef struct {
	FieldName string
	TypeName  string
	LexName   string
}

func (gen *CodeGenerator) writeUnion(ft *FlatType, union *lexicon.SchemaUnion) error {

	name := gen.baseName()
	if ft.DefName != "main" {
		name += "_" + strings.Title(ft.DefName)
	}
	for _, sub := range ft.Path {
		name += "_" + strings.Title(sub)
	}

	unionRefs := map[string]unionRef{}
	refNames := []string{}
	for _, ref := range union.Refs {
		r := unionRef{
			LexName: ref,
		}

		if strings.HasPrefix(ref, "#") {
			r.LexName = gen.Lex.NSID.String() + ref
			n := gen.baseName()
			if ref != "#main" {
				n += "_" + strings.Title(ref[1:])
			}
			r.FieldName = n
			r.TypeName = n
		} else {
			n, err := gen.externalRefType(ref)
			if err != nil {
				return err
			}
			r.FieldName = n
			r.TypeName = n
			if strings.Contains(n, ".") {
				parts := strings.SplitN(n, ".", 2)
				r.FieldName = parts[1]
			}
		}
		refNames = append(refNames, r.FieldName)
		unionRefs[r.FieldName] = r
	}
	sort.Strings(refNames)

	// Union dispatch uses the glex runtime.
	rt := gen.regAlias()

	// first print out the union struct type
	if union.Description != nil {
		for _, l := range strings.Split(*union.Description, "\n") {
			fmt.Fprintf(gen.Out, "// %s\n", l)
		}
	}
	fmt.Fprintf(gen.Out, "type %s struct {\n", name)
	for _, rname := range refNames {
		ref := unionRefs[rname]
		fmt.Fprintf(gen.Out, "\t%s *%s\n", ref.FieldName, ref.TypeName)
	}
	fmt.Fprintf(gen.Out, "\t// Raw preserves a variant whose $type is not in this union's generated\n")
	fmt.Fprintf(gen.Out, "\t// set, so unrecognized variants still round-trip losslessly through\n")
	fmt.Fprintf(gen.Out, "\t// decode/re-encode. Nil when a known variant is set.\n")
	fmt.Fprintf(gen.Out, "\tRaw *%s.RawRecord\n", rt)
	fmt.Fprintf(gen.Out, "}\n\n")

	// ... then MarshalJSON
	fmt.Fprintf(gen.Out, "// MarshalJSON emits the set variant, stamped with its $type, per the atproto\n")
	fmt.Fprintf(gen.Out, "// union wire format. The value receiver stamps a copy, so the variant is\n")
	fmt.Fprintf(gen.Out, "// never mutated and both %s and *%s marshal correctly.\n", name, name)
	fmt.Fprintf(gen.Out, "func (t %s) MarshalJSON() ([]byte, error) {\n", name)
	for _, rname := range refNames {
		ref := unionRefs[rname]
		fmt.Fprintf(gen.Out, "\tif t.%s != nil {\n", ref.FieldName)
		fmt.Fprintf(gen.Out, "\t\tcp := *t.%s\n", ref.FieldName)
		fmt.Fprintf(gen.Out, "\t\tcp.LexiconTypeID = \"%s\"\n", ref.LexName)
		fmt.Fprintf(gen.Out, "\t\treturn json.Marshal(&cp)\n")
		fmt.Fprintf(gen.Out, "\t}\n")
	}
	fmt.Fprintf(gen.Out, "\tif t.Raw != nil {\n")
	fmt.Fprintf(gen.Out, "\t\tif t.Raw.Encoding != \"json\" {\n")
	fmt.Fprintf(gen.Out, "\t\t\treturn nil, fmt.Errorf(\"cannot marshal raw %%s record as JSON in union %s\", t.Raw.Encoding)\n", name)
	fmt.Fprintf(gen.Out, "\t\t}\n")
	fmt.Fprintf(gen.Out, "\t\treturn t.Raw.Bytes, nil\n")
	fmt.Fprintf(gen.Out, "\t}\n")
	fmt.Fprintf(gen.Out, "\treturn nil, fmt.Errorf(\"cannot marshal empty union %s as JSON\")\n", name)
	fmt.Fprintf(gen.Out, "}\n\n")

	// ... then UnmarshalJSON
	fmt.Fprintf(gen.Out, "func (t *%s) UnmarshalJSON(b []byte) error {\n", name)
	fmt.Fprintf(gen.Out, "\ttyp, err := %s.TypeExtract(b)\n", rt)
	fmt.Fprintf(gen.Out, "\tif err != nil {\n")
	fmt.Fprintf(gen.Out, "\t\treturn err\n")
	fmt.Fprintf(gen.Out, "\t}\n\n")
	fmt.Fprintf(gen.Out, "\tswitch typ {\n")
	for _, rname := range refNames {
		ref := unionRefs[rname]
		fmt.Fprintf(gen.Out, "\tcase \"%s\":\n", ref.LexName)
		fmt.Fprintf(gen.Out, "\t\tt.%s = new(%s)\n", ref.FieldName, ref.TypeName)
		fmt.Fprintf(gen.Out, "\t\treturn json.Unmarshal(b, t.%s)\n", ref.FieldName)
	}
	fmt.Fprintf(gen.Out, "\tdefault:\n")
	if union.Closed != nil && *union.Closed {
		fmt.Fprintf(gen.Out, "\t\treturn fmt.Errorf(\"closed union %s: unrecognized $type %%q\", typ)\n", name)
	} else {
		fmt.Fprintf(gen.Out, "\t\tt.Raw = &%s.RawRecord{Type: typ, Encoding: \"json\", Bytes: append([]byte(nil), b...)}\n", rt)
		fmt.Fprintf(gen.Out, "\t\treturn nil\n")
	}
	fmt.Fprintf(gen.Out, "\t}\n")
	fmt.Fprintf(gen.Out, "}\n\n")

	// ... then MarshalCBOR. This is drisl-shaped (MarshalCBOR() ([]byte, error)),
	// NOT cbg-shaped: go-dasl only invokes drisl.Marshaler during reflection, so
	// a cbg-shaped method on a union nested inside a record would be silently
	// ignored and the union would serialize as its Go wrapper struct instead of
	// the atproto single-variant form.
	fmt.Fprintf(gen.Out, "// MarshalCBOR implements drisl.Marshaler, emitting the set variant (stamped\n")
	fmt.Fprintf(gen.Out, "// with its $type) per the atproto union wire format. go-dasl invokes this\n")
	fmt.Fprintf(gen.Out, "// when the union appears inside another record, so nested unions serialize\n")
	fmt.Fprintf(gen.Out, "// correctly.\n")
	fmt.Fprintf(gen.Out, "func (t %s) MarshalCBOR() ([]byte, error) {\n", name)
	for _, rname := range refNames {
		ref := unionRefs[rname]
		fmt.Fprintf(gen.Out, "\tif t.%s != nil {\n", ref.FieldName)
		fmt.Fprintf(gen.Out, "\t\tcp := *t.%s\n", ref.FieldName)
		fmt.Fprintf(gen.Out, "\t\tcp.LexiconTypeID = \"%s\"\n", ref.LexName)
		fmt.Fprintf(gen.Out, "\t\treturn %s.MarshalCBORBytes(&cp)\n", rt)
		fmt.Fprintf(gen.Out, "\t}\n")
	}
	fmt.Fprintf(gen.Out, "\tif t.Raw != nil {\n")
	fmt.Fprintf(gen.Out, "\t\tif t.Raw.Encoding != \"cbor\" {\n")
	fmt.Fprintf(gen.Out, "\t\t\treturn nil, fmt.Errorf(\"cannot marshal raw %%s record as CBOR in union %s\", t.Raw.Encoding)\n", name)
	fmt.Fprintf(gen.Out, "\t\t}\n")
	fmt.Fprintf(gen.Out, "\t\treturn t.Raw.Bytes, nil\n")
	fmt.Fprintf(gen.Out, "\t}\n")
	fmt.Fprintf(gen.Out, "\treturn nil, fmt.Errorf(\"cannot marshal empty union %s as CBOR\")\n", name)
	fmt.Fprintf(gen.Out, "}\n\n")

	// ... then UnmarshalCBOR (drisl-shaped, matching MarshalCBOR)
	fmt.Fprintf(gen.Out, "func (t *%s) UnmarshalCBOR(b []byte) error {\n", name)
	fmt.Fprintf(gen.Out, "\ttyp, err := %s.CborTypeExtract(b)\n", rt)
	fmt.Fprintf(gen.Out, "\tif err != nil {\n")
	fmt.Fprintf(gen.Out, "\t\treturn err\n")
	fmt.Fprintf(gen.Out, "\t}\n\n")
	fmt.Fprintf(gen.Out, "\tswitch typ {\n")
	for _, rname := range refNames {
		ref := unionRefs[rname]
		fmt.Fprintf(gen.Out, "\tcase \"%s\":\n", ref.LexName)
		fmt.Fprintf(gen.Out, "\t\tt.%s = new(%s)\n", ref.FieldName, ref.TypeName)
		fmt.Fprintf(gen.Out, "\t\treturn %s.UnmarshalCBORBytes(b, t.%s)\n", rt, ref.FieldName)
	}
	fmt.Fprintf(gen.Out, "\tdefault:\n")
	if union.Closed != nil && *union.Closed {
		fmt.Fprintf(gen.Out, "\t\treturn fmt.Errorf(\"closed union %s: unrecognized $type %%q\", typ)\n", name)
	} else {
		fmt.Fprintf(gen.Out, "\t\tt.Raw = &%s.RawRecord{Type: typ, Encoding: \"cbor\", Bytes: append([]byte(nil), b...)}\n", rt)
		fmt.Fprintf(gen.Out, "\t\treturn nil\n")
	}
	fmt.Fprintf(gen.Out, "\t}\n")
	fmt.Fprintf(gen.Out, "}\n\n")

	return nil
}

func (gen *CodeGenerator) writeEndpoint(ft *FlatType, desc string, params *lexicon.SchemaParams, output, input *lexicon.SchemaBody, isProcedure bool) error {
	name := gen.baseName()
	// XRPC client/dispatch uses the glex runtime.
	rt := gen.regAlias()

	fmt.Fprintf(gen.Out, "// %s calls the XRPC method \"%s\".\n", name, gen.Lex.NSID)
	if desc != "" {
		fmt.Fprintln(gen.Out, "//")
		for _, l := range strings.Split(desc, "\n") {
			fmt.Fprintf(gen.Out, "// %s\n", l)
		}
	}

	outputBytes := false
	outputStruct := ""
	if output != nil && output.Schema != nil {
		switch v := output.Schema.Inner.(type) {
		case lexicon.SchemaObject, lexicon.SchemaUnion:
			outputStruct = name + "_Output"
		case lexicon.SchemaRef:
			if strings.HasPrefix(v.Ref, "#") {
				outputStruct = fmt.Sprintf("%s_%s", gen.baseName(), strings.Title(v.Ref[1:]))
			} else {
				t, err := gen.externalRefType(v.Ref)
				if err != nil {
					return err
				}
				outputStruct = t
			}
		default:
			return fmt.Errorf("unsupported endpoint output schema def type: %T", output.Schema.Inner)
		}
	} else if output != nil && output.Encoding != "" {
		outputBytes = true
	}

	paramNames := []string{}
	if params != nil {
		for name := range params.Properties {
			paramNames = append(paramNames, name)
		}
	}
	sort.Strings(paramNames)

	args := []string{"ctx context.Context", "c " + rt + ".LexClient"}
	reqParams := []string{}
	optParams := []string{}
	if len(paramNames) > 0 {
		fmt.Fprintln(gen.Out, "//")
		for _, name := range paramNames {
			param := params.Properties[name]
			ptr := "*"
			if isRequired(params.Required, name) {
				ptr = ""
				reqParams = append(reqParams, name)
			} else {
				optParams = append(optParams, name)
			}
			switch v := param.Inner.(type) {
			case lexicon.SchemaBoolean:
				if v.Description != nil && *v.Description != "" {
					fmt.Fprintf(gen.Out, "// %s: %s\n", name, *v.Description)
				}
				args = append(args, fmt.Sprintf("%s %sbool", name, ptr))
			case lexicon.SchemaInteger:
				if v.Description != nil && *v.Description != "" {
					fmt.Fprintf(gen.Out, "// %s: %s\n", name, *v.Description)
				}
				args = append(args, fmt.Sprintf("%s %sint64", name, ptr))
			case lexicon.SchemaString:
				if v.Description != nil && *v.Description != "" {
					fmt.Fprintf(gen.Out, "// %s: %s\n", name, *v.Description)
				}
				args = append(args, fmt.Sprintf("%s string", name))
			case lexicon.SchemaUnknown:
				if v.Description != nil && *v.Description != "" {
					fmt.Fprintf(gen.Out, "// %s: %s\n", name, *v.Description)
				}
				args = append(args, fmt.Sprintf("%s any", name))
			case lexicon.SchemaArray:
				if v.Description != nil && *v.Description != "" {
					fmt.Fprintf(gen.Out, "// %s[]: %s\n", name, *v.Description)
				}
				switch v.Items.Inner.(type) {
				case lexicon.SchemaBoolean:
					args = append(args, fmt.Sprintf("%s []bool", name))
				case lexicon.SchemaInteger:
					args = append(args, fmt.Sprintf("%s []int64", name))
				case lexicon.SchemaString:
					args = append(args, fmt.Sprintf("%s []string", name))
				default:
					return fmt.Errorf("unsupported parameter array type: %T", param.Inner)
				}
			default:
				return fmt.Errorf("unsupported parameter type: %T", param.Inner)
			}
		}
	}

	inputArg := "nil"
	inputEncoding := ""
	inputStruct := ""
	if isProcedure && input != nil && input.Schema != nil {
		inputArg = "input"
		inputEncoding = input.Encoding
		switch v := input.Schema.Inner.(type) {
		case lexicon.SchemaObject, lexicon.SchemaUnion:
			inputStruct = name + "_Input"
		case lexicon.SchemaRef:
			if strings.HasPrefix(v.Ref, "#") {
				inputStruct = fmt.Sprintf("%s_%s", gen.baseName(), strings.Title(v.Ref[1:]))
			} else {
				t, err := gen.externalRefType(v.Ref)
				if err != nil {
					return err
				}
				inputStruct = t
			}
		}
		args = append(args, fmt.Sprintf("input *%s", inputStruct))
	} else if isProcedure && input != nil && input.Encoding != "" {
		inputArg = "input"
		inputEncoding = input.Encoding
		args = append(args, "input io.Reader")
	}

	doOutParam := ""
	returnType := ""
	fmt.Fprintf(gen.Out, "func %s(%s) ", name, strings.Join(args, ", "))
	if outputStruct != "" {
		fmt.Fprintf(gen.Out, "(*%s, error) {\n", outputStruct)
		fmt.Fprintf(gen.Out, "\tvar out %s\n", outputStruct)
		fmt.Fprintln(gen.Out, "")
		doOutParam = "&out"
		returnType = "&out"
	} else if outputBytes {
		fmt.Fprintf(gen.Out, "([]byte, error) {\n")
		fmt.Fprintf(gen.Out, "\tbuf := new(bytes.Buffer)\n\n")
		doOutParam = "buf"
		returnType = "buf.Bytes()"
	} else {
		fmt.Fprintf(gen.Out, "error {\n")
		doOutParam = "nil"
	}
	paramsArg := "nil"
	if params != nil && len(params.Properties) > 0 {
		paramsArg = "params"
		fmt.Fprintf(gen.Out, "\tparams := map[string]interface{}{}\n")
	}
	for _, name := range optParams {
		param := params.Properties[name]
		switch param.Inner.(type) {
		case lexicon.SchemaString:
			fmt.Fprintf(gen.Out, "\tif %s != \"\" {\n", name)
			fmt.Fprintf(gen.Out, "\t\tparams[\"%s\"] = %s\n", name, name)
			fmt.Fprintf(gen.Out, "\t}\n")
		case lexicon.SchemaArray:
			fmt.Fprintf(gen.Out, "\tif len(%s) != 0 {\n", name)
			fmt.Fprintf(gen.Out, "\t\tparams[\"%s\"] = %s\n", name, name)
			fmt.Fprintf(gen.Out, "\t}\n")
		case lexicon.SchemaUnknown:
			fmt.Fprintf(gen.Out, "\tif %s != nil {\n", name)
			fmt.Fprintf(gen.Out, "\t\tparams[\"%s\"] = %s\n", name, name)
			fmt.Fprintf(gen.Out, "\t}\n")
		case lexicon.SchemaInteger:
			fmt.Fprintf(gen.Out, "\tif %s != nil {\n", name)
			fmt.Fprintf(gen.Out, "\t\tparams[\"%s\"] = *%s\n", name, name)
			fmt.Fprintf(gen.Out, "\t}\n")
		case lexicon.SchemaBoolean:
			fmt.Fprintf(gen.Out, "\tif %s != nil {\n", name)
			fmt.Fprintf(gen.Out, "\t\tparams[\"%s\"] = *%s\n", name, name)
			fmt.Fprintf(gen.Out, "\t}\n")
		default:
			fmt.Fprintf(gen.Out, "\tif %s != nil {\n", name)
			fmt.Fprintf(gen.Out, "\t\tparams[\"%s\"] = *%s\n", name, name)
			fmt.Fprintf(gen.Out, "\t}\n")
		}
	}
	for _, name := range reqParams {
		fmt.Fprintf(gen.Out, "\tparams[\"%s\"] = %s\n", name, name)
	}
	fmt.Fprintln(gen.Out, "")

	method := rt + ".Query"
	if isProcedure {
		method = rt + ".Procedure"
	}

	fmt.Fprintf(gen.Out, "\tif err := c.LexDo(ctx, %s, \"%s\", \"%s\", %s, %s, %s); err != nil {\n", method, inputEncoding, gen.Lex.NSID, paramsArg, inputArg, doOutParam)
	if returnType != "" {
		fmt.Fprintf(gen.Out, "\t\treturn nil, err\n")
	} else {
		fmt.Fprintf(gen.Out, "\t\treturn err\n")
	}
	fmt.Fprintf(gen.Out, "\t}\n")
	if returnType != "" {
		fmt.Fprintf(gen.Out, "\treturn %s, nil\n", returnType)
	} else {
		fmt.Fprintf(gen.Out, "\treturn nil\n")
	}
	fmt.Fprintf(gen.Out, "}\n\n")

	return nil
}
