package parser

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

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
					declRaw := strings.TrimSpace(content[len("DIMS"):])
					declPart, initPart := splitDimDeclAndInit(declRaw)
					decl, ok := parseDimDecl(declPart, true, "global")
					if ok {
						if initPart != "" {
							if err := addDimInitializers(result.Defines, decl.Name, initPart); err != nil {
								return nil, fmt.Errorf("%s:%d: %w", line.File, line.Number, err)
							}
							// `#DIMS ... = "a", "b"` form: infer 1D length from initializer.
							// Explicit dimensions keep precedence.
							if !strings.Contains(declPart, ",") {
								decl.Dims = []int{max(1, len(splitTopLevel(initPart, ',')))}
							}
						}
						result.StringVars[strings.ToUpper(decl.Name)] = struct{}{}
						result.VarDecls = append(result.VarDecls, decl)
					}
					continue
				}
				if strings.HasPrefix(upper, "DIM ") {
					declRaw := strings.TrimSpace(content[len("DIM"):])
					declPart, initPart := splitDimDeclAndInit(declRaw)
					decl, ok := parseDimDecl(declPart, false, "global")
					if ok {
						if initPart != "" {
							if err := addDimInitializers(result.Defines, decl.Name, initPart); err != nil {
								return nil, fmt.Errorf("%s:%d: %w", line.File, line.Number, err)
							}
							if !strings.Contains(declPart, ",") {
								decl.Dims = []int{max(1, len(splitTopLevel(initPart, ',')))}
							}
						}
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

func splitDimDeclAndInit(raw string) (declPart string, initPart string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	depth := 0
	inStr := false
	escape := false
	for i, r := range raw {
		if inStr {
			if escape {
				escape = false
				continue
			}
			if r == '\\' {
				escape = true
				continue
			}
			if r == '"' {
				inStr = false
			}
			continue
		}
		switch r {
		case '"':
			inStr = true
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '=':
			if depth == 0 {
				return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+utf8.RuneLen(r):])
			}
		}
	}
	return raw, ""
}

func addDimInitializers(dst map[string]ast.Expr, name, initRaw string) error {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return nil
	}
	parts := splitTopLevel(initRaw, ',')
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		expr, err := ParseExpr(p)
		if err != nil {
			// keep compatibility with old parser behavior for bare numerics/strings
			if n, convErr := strconv.ParseInt(p, 10, 64); convErr == nil {
				expr = ast.IntLit{Value: n}
			} else {
				expr = ast.StringLit{Value: strings.Trim(p, "\"")}
			}
		}
		dst[fmt.Sprintf("%s:%d", name, i)] = expr
	}
	return nil
}
