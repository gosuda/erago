package parser

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gosuda/erago/ast"
)

type ERHResult struct {
	Defines    map[string]ast.Expr
	StringVars map[string]struct{}
	VarDecls   []ast.VarDecl
}

func ParseERH(files map[string]string, macros map[string]struct{}) (*ERHResult, error) {
	result := &ERHResult{
		Defines:    map[string]ast.Expr{},
		StringVars: map[string]struct{}{},
		VarDecls:   nil,
	}
	keys := sortedKeys(files)
	for _, file := range keys {
		raw := files[file]
		lines := preprocess(toLines(file, raw), macros)
		for _, line := range lines {
			if !strings.HasPrefix(strings.ToUpper(line.Content), "#") {
				continue
			}
			content := strings.TrimSpace(line.Content[1:])
			upper := strings.ToUpper(content)
			if !strings.HasPrefix(upper, "DEFINE") {
				if strings.HasPrefix(upper, "DIMS ") {
					decl, ok := parseDimDecl(content[len("DIMS"):], true, "global")
					if ok {
						result.StringVars[strings.ToUpper(decl.Name)] = struct{}{}
						result.VarDecls = append(result.VarDecls, decl)
					}
					continue
				}
				if strings.HasPrefix(upper, "DIM ") {
					decl, ok := parseDimDecl(content[len("DIM"):], false, "global")
					if ok {
						result.VarDecls = append(result.VarDecls, decl)
					}
					continue
				}
				continue
			}
			rest := strings.TrimSpace(content[len("DEFINE"):])
			if rest == "" {
				return nil, fmt.Errorf("%s:%d: invalid #DEFINE", line.File, line.Number)
			}
			name, exprRaw := splitNameAndRest(rest)
			if name == "" {
				return nil, fmt.Errorf("%s:%d: invalid #DEFINE name", line.File, line.Number)
			}
			uname := strings.ToUpper(name)
			var expr ast.Expr = ast.IntLit{Value: 1}
			if strings.TrimSpace(exprRaw) != "" {
				parsed, err := ParseExpr(exprRaw)
				if err != nil {
					return nil, fmt.Errorf("%s:%d: %w", line.File, line.Number, err)
				}
				expr = parsed
			}
			result.Defines[uname] = expr
			macros[uname] = struct{}{}
		}
	}
	return result, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func splitNameAndRest(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	for i, r := range raw {
		if r == ' ' || r == '\t' {
			return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+1:])
		}
	}
	return raw, ""
}
