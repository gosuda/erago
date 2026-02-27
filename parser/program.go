package parser

import (
	"fmt"
	"strings"

	"github.com/gosuda/erago/ast"
)

func ParseProgram(files map[string]string) (*ast.Program, error) {
	erh := map[string]string{}
	erb := map[string]string{}
	csv := map[string]string{}
	for file, content := range files {
		upper := strings.ToUpper(file)
		switch {
		case strings.HasSuffix(upper, ".ERH"):
			erh[file] = content
		case strings.HasSuffix(upper, ".ERB"):
			erb[file] = content
		case strings.HasSuffix(upper, ".CSV"):
			csv[file] = content
		}
	}

	if len(erb) == 0 {
		return nil, fmt.Errorf("no ERB files found")
	}

	macros := map[string]struct{}{}
	erhRes, err := ParseERH(erh, macros)
	if err != nil {
		return nil, err
	}

	res, err := ParseERB(erb, macros)
	if err != nil {
		return nil, err
	}

	return &ast.Program{
		Defines:    erhRes.Defines,
		Functions:  res.Functions,
		Order:      res.Order,
		CSVFiles:   csv,
		StringVars: erhRes.StringVars,
		VarDecls:   erhRes.VarDecls,
	}, nil
}
