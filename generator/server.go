package lexgen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// WriteServerStubs generates Echo HTTP handler stubs for all XRPC query and
// procedure endpoints in the given schemas. The output is a single Go file
// (stubs.go) with RegisterHandlers* functions and Handle* methods.
//
// This mirrors indigo's lexgen --gen-server output, adapted for glex's
// FlatLexicon/CodeGenerator types and the glex runtime.
func (gen *CodeGenerator) WriteServerStubs(allFlat []*FlatLexicon, pkgName string) error {
	// Group schemas by package prefix (the registered domain part of the NSID)
	prefixes := []string{}
	ssets := map[string][]*FlatLexicon{}
	for _, fl := range allFlat {
		pref := nsidPkgName(fl.NSID)
		if _, ok := ssets[pref]; !ok {
			prefixes = append(prefixes, pref)
		}
		ssets[pref] = append(ssets[pref], fl)
	}
	sort.Strings(prefixes)

	pf := func(format string, args ...any) {
		fmt.Fprintf(gen.Out, format, args...)
	}

	pf("package %s\n\n", pkgName)
	pf("import (\n")
	pf("\t\"context\"\n")
	pf("\t\"strconv\"\n")
	pf("\n")
	pf("\t\"github.com/labstack/echo/v4\"\n")
	pf("\t\"go.opentelemetry.io/otel\"\n")
	// Import all generated packages
	for _, pref := range prefixes {
		if pref == pkgName {
			continue
		}
		pf("\t%s \"%s/%s\"\n", pref, gen.Config.ModulePath, pref)
	}
	pf(")\n\n")

	for _, pref := range prefixes {
		ss := ssets[pref]
		impname := pref
		if pref == pkgName {
			impname = "" // same package, no qualifier
		}

		pf("func (s *Server) RegisterHandlers%s(e *echo.Echo) error {\n", idToTitle(pref))
		for _, fl := range ss {
			main, ok := fl.Defs["main"]
			if !ok {
				continue
			}
			var verb string
			switch main.Type {
			case "query":
				verb = "GET"
			case "procedure":
				verb = "POST"
			default:
				continue
			}
			pf("\te.%s(\"/xrpc/%s\", s.Handle%s)\n", verb, fl.NSID, idToTitle(fl.NSID.String()))
		}
		pf("\treturn nil\n}\n\n")

		for _, fl := range ss {
			main, ok := fl.Defs["main"]
			if !ok {
				continue
			}
			if main.Type != "query" && main.Type != "procedure" {
				continue
			}
			fname := idToTitle(fl.NSID.String())
			tname := nsidBaseName(fl.NSID)
			gen.Lex = fl // Set Lex so externalRefType can access gen.Lex.NSID
			if err := gen.writeRPCHandler(pf, fl, main, fname, tname, impname); err != nil {
				return err
			}
		}
	}

	return nil
}

func (gen *CodeGenerator) writeRPCHandler(pf func(string, ...any), fl *FlatLexicon, mainDef FlatDef, fname, tname, impname string) error {
	// Find the actual schema def for "main"
	var schema *lexicon.SchemaDef
	for _, ft := range fl.Types {
		if ft.DefName == "main" && len(ft.Path) == 0 {
			schema = ft.Schema
			break
		}
	}
	if schema == nil {
		return fmt.Errorf("could not find main def for %s", fl.NSID)
	}

	pf("func (s *Server) Handle%s(c echo.Context) error {\n", fname)
	pf("\tctx, span := otel.Tracer(\"server\").Start(c.Request().Context(), %q)\n", "Handle"+fname)
	pf("\tdefer span.End()\n")

	paramtypes := []string{"ctx context.Context"}
	params := []string{"ctx"}

	var query *lexicon.SchemaQuery
	var proc *lexicon.SchemaProcedure
	switch v := schema.Inner.(type) {
	case lexicon.SchemaQuery:
		query = &v
	case lexicon.SchemaProcedure:
		proc = &v
	}

	if mainDef.Type == "query" || mainDef.Type == "procedure" {
		// Parse query parameters for both queries and procedures (procedures
		// can have query params too, e.g. place.stream.playback.whep)
		var schemaParams *lexicon.SchemaParams
		if query != nil && query.Parameters != nil {
			schemaParams = query.Parameters
		} else if proc != nil && proc.Parameters != nil {
			schemaParams = proc.Parameters
		}
		if schemaParams != nil {
			required := map[string]bool{}
			for _, r := range schemaParams.Required {
				required[r] = true
			}
			// Sort param names for deterministic output
			paramNames := make([]string, 0, len(schemaParams.Properties))
			for k := range schemaParams.Properties {
				paramNames = append(paramNames, k)
			}
			sort.Strings(paramNames)

			for _, k := range paramNames {
				t := schemaParams.Properties[k]
				switch v := t.Inner.(type) {
				case lexicon.SchemaString:
					params = append(params, k)
					paramtypes = append(paramtypes, k+" string")
					pf("\t%s := c.QueryParam(\"%s\")\n", k, k)
				case lexicon.SchemaInteger:
					params = append(params, k)
					if !required[k] {
						// Optional integer: use int with default 0 (matching old indigo behavior)
						paramtypes = append(paramtypes, k+" int")
						pf("\t%s := 0\n", k)
						pf("\tif p := c.QueryParam(\"%s\"); p != \"\" {\n", k)
						pf("\t\tvar err error\n\t\t%s, err = strconv.Atoi(p)\n", k)
						pf("\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n\t}\n")
					} else if v.Default != nil {
						paramtypes = append(paramtypes, k+" int")
						pf("\tvar %s int\n", k)
						pf("\tif p := c.QueryParam(\"%s\"); p != \"\" {\n", k)
						pf("\t\tvar err error\n\t\t%s, err = strconv.Atoi(p)\n", k)
						pf("\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n\t} else {\n")
						pf("\t\t%s = %d\n\t}\n", k, *v.Default)
					} else {
						paramtypes = append(paramtypes, k+" int")
						pf("\t%s, err := strconv.Atoi(c.QueryParam(\"%s\"))\n", k, k)
						pf("\tif err != nil {\n\t\treturn err\n\t}\n")
					}
				case lexicon.SchemaBoolean:
					params = append(params, k)
					if !required[k] {
						// Optional boolean: use bool with default false
						paramtypes = append(paramtypes, k+" bool")
						pf("\t%s := false\n", k)
						pf("\tif p := c.QueryParam(\"%s\"); p != \"\" {\n", k)
						pf("\t\tvar err error\n\t\t%s, err = strconv.ParseBool(p)\n", k)
						pf("\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n\t}\n")
					} else if v.Default != nil {
						paramtypes = append(paramtypes, k+" bool")
						pf("\tvar %s bool\n", k)
						pf("\tif p := c.QueryParam(\"%s\"); p != \"\" {\n", k)
						pf("\t\tvar err error\n\t\t%s, err = strconv.ParseBool(p)\n", k)
						pf("\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n\t} else {\n")
						pf("\t\t%s = %v\n\t}\n", k, *v.Default)
					} else {
						paramtypes = append(paramtypes, k+" bool")
						pf("\t%s, err := strconv.ParseBool(c.QueryParam(\"%s\"))\n", k, k)
						pf("\tif err != nil {\n\t\treturn err\n\t}\n")
					}
				case lexicon.SchemaArray:
					switch v.Items.Inner.(type) {
					case lexicon.SchemaString:
						paramtypes = append(paramtypes, k+" []string")
						params = append(params, k)
						pf("\t%s := c.QueryParams()[\"%s\"]\n", k, k)
					default:
						return fmt.Errorf("unsupported handler array param type for %s.%s", fl.NSID, k)
					}
				default:
					return fmt.Errorf("unsupported handler param type for %s.%s: %T", fl.NSID, k, t.Inner)
				}
			}
		}
	}
	// Parse procedure input body (in addition to any query params above)
	if mainDef.Type == "procedure" {
		// Parse input body
		if proc != nil && proc.Input != nil {
			intname := tname + "_Input"
			if impname != "" {
				intname = impname + "." + tname + "_Input"
			}
			switch proc.Input.Encoding {
			case "application/json":
				pf("\tvar body %s\n", intname)
				pf("\tif err := c.Bind(&body); err != nil {\n\t\treturn err\n\t}\n")
				paramtypes = append(paramtypes, "body *"+intname)
				params = append(params, "&body")
			default:
				// Non-JSON input: pass as io.Reader + contentType
				pf("\tbody := c.Request().Body\n")
				pf("\tcontentType := c.Request().Header.Get(\"Content-Type\")\n")
				paramtypes = append(paramtypes, "r io.Reader", "contentType string")
				params = append(params, "body", "contentType")
			}
		}
	}

	// Determine output type
	assign := "handleErr"
	returndef := "error"

	var output *lexicon.SchemaBody
	if query != nil && query.Output != nil {
		output = query.Output
	} else if proc != nil && proc.Output != nil {
		output = proc.Output
	}

	if output != nil {
		switch output.Encoding {
		case "application/json":
			assign = "out, handleErr"
			outname := tname + "_Output"
			if output.Schema != nil {
				switch v := output.Schema.Inner.(type) {
				case lexicon.SchemaRef:
					if strings.HasPrefix(v.Ref, "#") {
						outname = fmt.Sprintf("%s_%s", tname, strings.Title(v.Ref[1:]))
					} else {
						var err error
						outname, err = gen.externalRefType(v.Ref)
						if err != nil {
							return err
						}
					}
				}
			}
			fullOut := outname
			if impname != "" && !strings.Contains(outname, ".") {
				fullOut = impname + "." + outname
			}
			pf("\tvar out *%s\n", fullOut)
			returndef = fmt.Sprintf("(*%s, error)", fullOut)
		default:
			assign = "out, handleErr"
			pf("\tvar out io.Reader\n")
			returndef = "(io.Reader, error)"
		}
	}

	pf("\tvar handleErr error\n")
	pf("\t// func (s *Server) handle%s(%s) %s\n", fname, strings.Join(paramtypes, ","), returndef)
	pf("\t%s = s.handle%s(%s)\n", assign, fname, strings.Join(params, ","))
	pf("\tif handleErr != nil {\n\t\treturn handleErr\n\t}\n")

	if output != nil {
		switch output.Encoding {
		case "application/json":
			pf("\treturn c.JSON(200, out)\n}\n\n")
		default:
			pf("\treturn c.Stream(200, \"application/octet-stream\", out)\n}\n\n")
		}
	} else {
		pf("\treturn nil\n}\n\n")
	}

	return nil
}

// idToTitle converts an NSID string to a Title-cased Go identifier.
// E.g. "com.atproto.repo.getRecord" → "ComAtprotoRepoGetRecord".
func idToTitle(id string) string {
	var fname string
	for _, p := range strings.Split(id, ".") {
		fname += strings.Title(p)
	}
	return fname
}

// nameFromID computes the short type name from an NSID, relative to a prefix.
func nameFromID(id string, prefix string) string {
	nsid, err := syntax.ParseNSID(id)
	if err != nil {
		return ""
	}
	return nsidBaseName(nsid)
}
