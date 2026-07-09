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

	// emit $type field
	if _, ok := obj.Properties["$type"]; !ok {
		hasType = true
		typeConst = gen.Lex.NSID.String()
		if len(ft.Path) > 0 {
			typeConst = gen.Lex.NSID.String() + "#" + ft.DefName
		}
		fmt.Fprintf(gen.Out, "\tLexiconTypeID string `json:\"$type\"`\n")
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

	gen.writeCBORAdapter(name, hasType, typeConst)

	return nil
}

// writeCBORAdapter emits cbg.CBORMarshalor-shaped methods that delegate to
// go-dasl (via the runtime), so generated structs interoperate with indigo's
// repo/carstore/MST layer while serializing as canonical DAG-CBOR through
// go-dasl.
func (gen *CodeGenerator) writeCBORAdapter(name string, hasType bool, typeConst string) {
	rt := gen.rtAlias()
	fmt.Fprintf(gen.Out, "func (t *%s) MarshalCBOR(w io.Writer) error {\n", name)
	fmt.Fprintf(gen.Out, "\tif t == nil {\n")
	fmt.Fprintf(gen.Out, "\t\t_, err := w.Write(cbg.CborNull)\n")
	fmt.Fprintf(gen.Out, "\t\treturn err\n")
	fmt.Fprintf(gen.Out, "\t}\n")
	if hasType {
		fmt.Fprintf(gen.Out, "\tt.LexiconTypeID = %q\n", typeConst)
	}
	fmt.Fprintf(gen.Out, "\treturn %s.MarshalCBOR(w, t)\n", rt)
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
	fmt.Fprintf(gen.Out, "}\n\n")

	// Union dispatch uses the glex runtime.
	rt := gen.regAlias()

	// ... then MarshalJSON
	fmt.Fprintf(gen.Out, "func (t *%s) MarshalJSON() ([]byte, error) {\n", name)
	for _, rname := range refNames {
		ref := unionRefs[rname]
		fmt.Fprintf(gen.Out, "\tif t.%s != nil {\n", ref.FieldName)
		fmt.Fprintf(gen.Out, "\t\tt.%s.LexiconTypeID = \"%s\"\n", ref.FieldName, ref.LexName)
		fmt.Fprintf(gen.Out, "\t\treturn json.Marshal(t.%s)\n", ref.FieldName)
		fmt.Fprintf(gen.Out, "\t}\n")
	}
	fmt.Fprintf(gen.Out, "\treturn nil, fmt.Errorf(\"can not marshal empty union as JSON\")")
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
		fmt.Fprintf(gen.Out, "\t\treturn fmt.Errorf(\"closed unions must match a listed schema\")\n")
	} else {
		fmt.Fprintf(gen.Out, "\t\treturn nil\n")
	}
	fmt.Fprintf(gen.Out, "\t}\n")
	fmt.Fprintf(gen.Out, "}\n\n")

	// ... then MarshalCBOR
	fmt.Fprintf(gen.Out, "func (t *%s) MarshalCBOR(w io.Writer) error {\n\n", name)
	fmt.Fprintf(gen.Out, "\tif t == nil {\n")
	fmt.Fprintf(gen.Out, "\t\t_, err := w.Write(cbg.CborNull)\n")
	fmt.Fprintf(gen.Out, "\t\treturn err")
	fmt.Fprintf(gen.Out, "\t}\n")
	for _, rname := range refNames {
		ref := unionRefs[rname]
		fmt.Fprintf(gen.Out, "\tif t.%s != nil {\n", ref.FieldName)
		fmt.Fprintf(gen.Out, "\t\treturn t.%s.MarshalCBOR(w)\n", ref.FieldName)
		fmt.Fprintf(gen.Out, "\t}\n")
	}
	fmt.Fprintf(gen.Out, "\treturn fmt.Errorf(\"can not marshal empty union as CBOR\")")
	fmt.Fprintf(gen.Out, "}\n\n")

	// ... then UnmarshalCBOR
	fmt.Fprintf(gen.Out, "func (t *%s) UnmarshalCBOR(r io.Reader) error {\n", name)
	fmt.Fprintf(gen.Out, "\ttyp, b, err := %s.CborTypeExtractReader(r)\n", rt)
	fmt.Fprintf(gen.Out, "\tif err != nil {\n")
	fmt.Fprintf(gen.Out, "\t\treturn err\n")
	fmt.Fprintf(gen.Out, "\t}\n\n")
	fmt.Fprintf(gen.Out, "\tswitch typ {\n")
	for _, rname := range refNames {
		ref := unionRefs[rname]
		fmt.Fprintf(gen.Out, "\tcase \"%s\":\n", ref.LexName)
		fmt.Fprintf(gen.Out, "\t\tt.%s = new(%s)\n", ref.FieldName, ref.TypeName)
		fmt.Fprintf(gen.Out, "\t\treturn t.%s.UnmarshalCBOR(bytes.NewReader(b))\n", ref.FieldName)
	}
	fmt.Fprintf(gen.Out, "\tdefault:\n")
	fmt.Fprintf(gen.Out, "\t\treturn nil\n")
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
