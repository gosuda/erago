package erago

import (
	"github.com/gosuda/erago/ast"
	"github.com/gosuda/erago/parser"
	eruntime "github.com/gosuda/erago/runtime"
)

// Compile parses ERH/ERB files and builds a VM instance.
// The input map key is the virtual file name (e.g. "MAIN.ERB").
func Compile(files map[string]string) (*eruntime.VM, error) {
	program, err := parser.ParseProgram(files)
	if err != nil {
		return nil, err
	}
	return eruntime.New(program)
}

// Parse only returns AST program for tooling use.
func Parse(files map[string]string) (*ast.Program, error) {
	return parser.ParseProgram(files)
}
