package parser

import (
	"strconv"
	"strings"

	"github.com/gosuda/erago/ast"
)

func parseDimDecl(raw string, isString bool, defaultScope string) (ast.VarDecl, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ast.VarDecl{}, false
	}
	parts := splitTopLevel(raw, ',')
	if len(parts) == 0 {
		return ast.VarDecl{}, false
	}

	headFields := strings.Fields(strings.TrimSpace(parts[0]))
	if len(headFields) == 0 {
		return ast.VarDecl{}, false
	}

	scope := strings.ToLower(strings.TrimSpace(defaultScope))
	if scope == "" {
		scope = "global"
	}
	isRef := false
	isDynamic := false

	for _, f := range headFields[:len(headFields)-1] {
		u := strings.ToUpper(strings.TrimSpace(f))
		switch u {
		case "GLOBAL", "SAVEDATA", "CHARADATA":
			scope = "global"
		case "LOCAL":
			scope = "local"
		case "DYNAMIC":
			isDynamic = true
		case "REF":
			isRef = true
		case "CONST":
			// currently ignored
		}
	}

	name := strings.ToUpper(strings.TrimSpace(headFields[len(headFields)-1]))
	if name == "" {
		return ast.VarDecl{}, false
	}

	dims := make([]int, 0, len(parts)-1)
	for _, p := range parts[1:] {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n := 1
		if v, err := strconv.Atoi(p); err == nil {
			n = v
		}
		if n < 1 {
			n = 1
		}
		dims = append(dims, n)
	}
	if len(dims) == 0 {
		dims = []int{1}
	}

	return ast.VarDecl{
		Name:      name,
		IsString:  isString,
		Dims:      dims,
		Scope:     scope,
		IsRef:     isRef,
		IsDynamic: isDynamic,
	}, true
}
