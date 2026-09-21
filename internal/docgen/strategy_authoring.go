// Package docgen generates the strategy-authoring knowledge document
// (Backend Spec 05) the AI strategy agent injects into its system prompt
// and serves as the docs://strategy-authoring MCP resource. It is a build
// -time tool, not part of any binary's runtime - see
// internal/docgen/cmd/gen-strategy-authoring-doc.
//
// The generator parses source directly (go/ast, not a hand-maintained
// schema file) so the document can never drift the way
// config.example.yml did: app/strategies/interface.go's Context/Strategy
// declarations and app/strategies/script/indicators.go's bindIndicators
// registrations are the single source of truth.
package docgen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	interfaceGoRelPath    = "app/strategies/interface.go"
	indicatorsGoRelPath   = "app/strategies/script/indicators.go"
	strategyEntityRelPath = "app/entities/strategy.go"
)

// GenerateStrategyAuthoringDoc parses app/strategies/interface.go's doc
// comments, app/strategies/script/indicators.go's bindIndicators
// registrations, and app/entities/strategy.go's Cycle/StrategyStatus enums,
// and renders a single markdown document combining: the Strategy/Context/
// Signal contract, the full ind.* method list, and config conventions
// (valid Cycle values, valid Status values, snake_case JSON key
// convention).
func GenerateStrategyAuthoringDoc(repoRoot string) (string, error) {
	fset := token.NewFileSet()

	ifaceFile, err := parser.ParseFile(fset, filepath.Join(repoRoot, interfaceGoRelPath), nil, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("docgen: failed to parse %s: %w", interfaceGoRelPath, err)
	}
	indicatorsFile, err := parser.ParseFile(fset, filepath.Join(repoRoot, indicatorsGoRelPath), nil, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("docgen: failed to parse %s: %w", indicatorsGoRelPath, err)
	}
	entityFile, err := parser.ParseFile(fset, filepath.Join(repoRoot, strategyEntityRelPath), nil, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("docgen: failed to parse %s: %w", strategyEntityRelPath, err)
	}

	contextFields, err := extractStructFields(fset, ifaceFile, "Context")
	if err != nil {
		return "", err
	}
	strategyMethods, err := extractInterfaceMethods(fset, ifaceFile, "Strategy")
	if err != nil {
		return "", err
	}
	signalFields, err := extractStructFields(fset, ifaceFile, "Signal")
	if err != nil {
		return "", err
	}
	executionModeDoc := typeDocComment(ifaceFile, "ExecutionMode")

	indicatorNames, err := extractIndicatorNames(indicatorsFile)
	if err != nil {
		return "", err
	}
	bindIndicatorsDoc := funcDocComment(indicatorsFile, "bindIndicators")

	cycles, err := extractIntConstEnum(entityFile, "Cycle")
	if err != nil {
		return "", err
	}
	statuses, err := extractStringConstEnum(entityFile, "StrategyStatus")
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("# Strategy Authoring Reference\n\n")
	b.WriteString("Generated from `app/strategies/interface.go` and `app/strategies/script/indicators.go` " +
		"by `internal/docgen` (Backend Spec 05, `ai-strategy-agent`). Do not hand-edit - run " +
		"`make gen-agent-docs` after changing either source file.\n\n")

	b.WriteString("## The Strategy contract\n\n")
	b.WriteString("Every strategy is a Lua script evaluated against a fixed lifecycle-hook contract. " +
		"`Strategy`/`Context`/`Signal`/`ExecutionMode` (`app/strategies/interface.go`) are frozen - " +
		"the hook signatures below never change.\n\n")
	b.WriteString("### Hooks\n\n")
	for _, m := range strategyMethods {
		b.WriteString(fmt.Sprintf("- `%s`", m.signature))
		if m.doc != "" {
			b.WriteString(" - " + m.doc)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n### Context fields\n\n")
	b.WriteString("Every hook receives a `Context` with exactly these fields:\n\n")
	for _, f := range contextFields {
		b.WriteString(fmt.Sprintf("- `%s %s`", f.name, f.typ))
		if f.doc != "" {
			b.WriteString(" - " + f.doc)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n### Signal fields\n\n")
	b.WriteString("`GoLong`/`GoShort`/`UpdatePosition` return a `Signal`:\n\n")
	for _, f := range signalFields {
		b.WriteString(fmt.Sprintf("- `%s %s`", f.name, f.typ))
		if f.doc != "" {
			b.WriteString(" - " + f.doc)
		}
		b.WriteString("\n")
	}

	if executionModeDoc != "" {
		b.WriteString("\n### Execution mode\n\n")
		b.WriteString(executionModeDoc + "\n")
	}

	b.WriteString(fmt.Sprintf("\n## Indicator catalogue (%d methods)\n\n", len(indicatorNames)))
	b.WriteString("Every `ind.*` closure available to a Lua script, bound fresh per cycle over the " +
		"current candle window:\n\n")
	if bindIndicatorsDoc != "" {
		b.WriteString(bindIndicatorsDoc + "\n\n")
	}
	for _, name := range indicatorNames {
		b.WriteString(fmt.Sprintf("- `ind.%s` - see `app/strategies/script/indicators.go` for its exact signature.\n", name))
	}

	b.WriteString("\n## Config conventions\n\n")
	b.WriteString("### Valid `cycle_minutes` values\n\n")
	for _, c := range cycles {
		b.WriteString(fmt.Sprintf("- `%d` (%s)\n", c.value, c.name))
	}
	b.WriteString("\n### Valid `status` values\n\n")
	b.WriteString("A script authored by the agent is always saved with `status = \"testing\"` - the agent " +
		"has no channel to request any other status. For reference, the full enum is:\n\n")
	for _, s := range statuses {
		b.WriteString(fmt.Sprintf("- `%s` (%s)\n", s.value, s.name))
	}

	b.WriteString("\n### JSON key convention\n\n")
	b.WriteString("Every tool input/output uses snake_case keys (`strategy_id`, `script_source`, " +
		"`cycle_minutes`), matching every other REST DTO in this codebase.\n")

	return b.String(), nil
}

type docField struct {
	name string
	typ  string
	doc  string
}

type docMethod struct {
	signature string
	doc       string
}

type constEnumInt struct {
	name  string
	value int
}

type constEnumString struct {
	name  string
	value string
}

func exprString(fset *token.FileSet, expr ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, expr); err != nil {
		return ""
	}
	return buf.String()
}

func cleanDoc(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	text := strings.TrimSpace(cg.Text())
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " ")
}

func findTypeSpec(file *ast.File, name string) (*ast.TypeSpec, *ast.GenDecl) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			return ts, gd
		}
	}
	return nil, nil
}

func typeDocComment(file *ast.File, name string) string {
	ts, gd := findTypeSpec(file, name)
	if ts == nil {
		return ""
	}
	if ts.Doc != nil {
		return cleanDoc(ts.Doc)
	}
	return cleanDoc(gd.Doc)
}

func extractStructFields(fset *token.FileSet, file *ast.File, typeName string) ([]docField, error) {
	ts, _ := findTypeSpec(file, typeName)
	if ts == nil {
		return nil, fmt.Errorf("docgen: type %s not found", typeName)
	}
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return nil, fmt.Errorf("docgen: %s is not a struct", typeName)
	}
	var fields []docField
	for _, f := range st.Fields.List {
		typ := exprString(fset, f.Type)
		doc := cleanDoc(f.Doc)
		if doc == "" {
			doc = cleanDoc(f.Comment)
		}
		if len(f.Names) == 0 {
			// Embedded field.
			fields = append(fields, docField{name: typ, typ: "", doc: doc})
			continue
		}
		for _, n := range f.Names {
			fields = append(fields, docField{name: n.Name, typ: typ, doc: doc})
		}
	}
	return fields, nil
}

func extractInterfaceMethods(fset *token.FileSet, file *ast.File, typeName string) ([]docMethod, error) {
	ts, _ := findTypeSpec(file, typeName)
	if ts == nil {
		return nil, fmt.Errorf("docgen: type %s not found", typeName)
	}
	it, ok := ts.Type.(*ast.InterfaceType)
	if !ok {
		return nil, fmt.Errorf("docgen: %s is not an interface", typeName)
	}
	var methods []docMethod
	for _, m := range it.Methods.List {
		if len(m.Names) == 0 {
			continue
		}
		ft, ok := m.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		sig := m.Names[0].Name + exprString(fset, ft)[len("func"):]
		doc := cleanDoc(m.Doc)
		if doc == "" {
			doc = cleanDoc(m.Comment)
		}
		methods = append(methods, docMethod{signature: sig, doc: doc})
	}
	return methods, nil
}

func funcDocComment(file *ast.File, funcName string) string {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != funcName {
			continue
		}
		return cleanDoc(fd.Doc)
	}
	return ""
}

// extractIndicatorNames walks bindIndicators' body for every
// `L.SetField(ind, "<name>", ...)` call - the authoritative registration
// site, not the hand-written prose in the function's doc comment - so the
// generated catalogue and its count can never silently drift from what is
// actually exposed to Lua (Backend Spec 05 AC#2/#5).
func extractIndicatorNames(file *ast.File) ([]string, error) {
	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if ok && fd.Name.Name == "bindIndicators" {
			fn = fd
			break
		}
	}
	if fn == nil {
		return nil, fmt.Errorf("docgen: bindIndicators function not found")
	}

	names := map[string]struct{}{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "SetField" {
			return true
		}
		if len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[1].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		name, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		names[name] = struct{}{}
		return true
	})

	if len(names) == 0 {
		return nil, fmt.Errorf("docgen: no ind.* registrations found in bindIndicators - every indicator " +
			"must be discoverable, never silently omitted")
	}

	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func extractIntConstEnum(file *ast.File, typeName string) ([]constEnumInt, error) {
	var result []constEnumInt
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != typeName {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					continue
				}
				val, err := strconv.Atoi(lit.Value)
				if err != nil {
					continue
				}
				result = append(result, constEnumInt{name: name.Name, value: val})
			}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("docgen: no %s const values found", typeName)
	}
	return result, nil
}

func extractStringConstEnum(file *ast.File, typeName string) ([]constEnumString, error) {
	var result []constEnumString
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != typeName {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				result = append(result, constEnumString{name: name.Name, value: val})
			}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("docgen: no %s const values found", typeName)
	}
	return result, nil
}
