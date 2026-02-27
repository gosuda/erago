package eruntime

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gosuda/erago/ast"
)

func (vm *VM) evalExpr(e ast.Expr) (Value, error) {
	switch ex := e.(type) {
	case ast.IntLit:
		return Int(ex.Value), nil
	case ast.StringLit:
		return Str(ex.Value), nil
	case ast.VarRef:
		return vm.getVarRef(ex)
	case ast.UnaryExpr:
		v, err := vm.evalExpr(ex.Expr)
		if err != nil {
			return Value{}, err
		}
		switch ex.Op {
		case "+":
			return Int(v.Int64()), nil
		case "-":
			return Int(-v.Int64()), nil
		case "!":
			if v.Truthy() {
				return Int(0), nil
			}
			return Int(1), nil
		case "~":
			return Int(^v.Int64()), nil
		default:
			return Value{}, fmt.Errorf("unsupported unary operator %q", ex.Op)
		}
	case ast.BinaryExpr:
		switch ex.Op {
		case "&&":
			left, err := vm.evalExpr(ex.Left)
			if err != nil {
				return Value{}, err
			}
			if !left.Truthy() {
				return Int(0), nil
			}
			right, err := vm.evalExpr(ex.Right)
			if err != nil {
				return Value{}, err
			}
			if right.Truthy() {
				return Int(1), nil
			}
			return Int(0), nil
		case "||":
			left, err := vm.evalExpr(ex.Left)
			if err != nil {
				return Value{}, err
			}
			if left.Truthy() {
				return Int(1), nil
			}
			right, err := vm.evalExpr(ex.Right)
			if err != nil {
				return Value{}, err
			}
			if right.Truthy() {
				return Int(1), nil
			}
			return Int(0), nil
		case "!&":
			left, err := vm.evalExpr(ex.Left)
			if err != nil {
				return Value{}, err
			}
			if !left.Truthy() {
				return Int(1), nil
			}
			right, err := vm.evalExpr(ex.Right)
			if err != nil {
				return Value{}, err
			}
			if right.Truthy() {
				return Int(0), nil
			}
			return Int(1), nil
		case "!|":
			left, err := vm.evalExpr(ex.Left)
			if err != nil {
				return Value{}, err
			}
			if left.Truthy() {
				return Int(0), nil
			}
			right, err := vm.evalExpr(ex.Right)
			if err != nil {
				return Value{}, err
			}
			if right.Truthy() {
				return Int(0), nil
			}
			return Int(1), nil
		default:
			left, err := vm.evalExpr(ex.Left)
			if err != nil {
				return Value{}, err
			}
			right, err := vm.evalExpr(ex.Right)
			if err != nil {
				return Value{}, err
			}
			return evalBinary(ex.Op, left, right)
		}
	case ast.TernaryExpr:
		cond, err := vm.evalExpr(ex.Cond)
		if err != nil {
			return Value{}, err
		}
		if cond.Truthy() {
			return vm.evalExpr(ex.True)
		}
		return vm.evalExpr(ex.False)
	case ast.CallExpr:
		return vm.evalCallExpr(ex)
	case ast.EmptyLit:
		return Int(0), nil
	case ast.IncDecExpr:
		cur, err := vm.getVarRef(ex.Target)
		if err != nil {
			return Value{}, err
		}
		delta := int64(1)
		if ex.Op == "--" {
			delta = -1
		}
		next := Int(cur.Int64() + delta)
		if err := vm.setVarRef(ex.Target, next); err != nil {
			return Value{}, err
		}
		if ex.Post {
			return cur, nil
		}
		return next, nil
	default:
		return Value{}, fmt.Errorf("unsupported expression %T", e)
	}
}

func (vm *VM) evalCallExpr(ex ast.CallExpr) (Value, error) {
	name := strings.ToUpper(strings.TrimSpace(ex.Name))
	rawExprArg := callExprExprArg(ex.Args)
	args, missing, err := vm.evalCallExprArgs(ex.Args)
	if err != nil {
		return Value{}, err
	}
	rawArg := callExprRawArg(args, missing)

	if shouldPreferMethodLike(name) {
		if v, handled, err := vm.execMethodLike(name, rawExprArg); handled {
			return v, err
		}
	}
	if vm.program.Functions[name] != nil {
		if _, err := vm.callFunctionArgs(name, args, missing); err != nil {
			if v, handled, mErr := vm.execMethodLike(name, rawExprArg); handled && mErr == nil {
				return v, nil
			}
			return Value{}, err
		}
		return vm.getVar("RESULT"), nil
	}
	if v, handled, err := vm.execMethodLike(name, rawExprArg); handled {
		return v, err
	}
	if res, err := vm.runCommandStatement(ast.CommandStmt{Name: name, Arg: rawArg}); err == nil {
		if res.kind == resultNone {
			return vm.getVar("RESULT"), nil
		}
	} else {
		return Value{}, err
	}
	return Value{}, fmt.Errorf("unknown expression call %s", name)
}

func (vm *VM) evalCallExprArgs(exprs []ast.Expr) ([]Value, []bool, error) {
	args := make([]Value, 0, len(exprs))
	missing := make([]bool, 0, len(exprs))
	for _, ae := range exprs {
		if _, ok := ae.(ast.EmptyLit); ok {
			args = append(args, Int(0))
			missing = append(missing, true)
			continue
		}
		v, err := vm.evalExpr(ae)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, v)
		missing = append(missing, false)
	}
	return args, missing, nil
}

func shouldPreferMethodLike(name string) bool {
	switch name {
	case "HTMLP", "HTMLFONT", "HTMLSTYLE", "HTMLNOBR", "HTMLCOLOR", "HTMLBUTTON", "HTMLAUTOBUTTON", "HTMLNONBUTTON":
		return true
	case "REGEXPMATCH", "HTML_STRINGLEN", "HTML_SUBSTRING", "HTML_STRINGLINES":
		return true
	case "ISDEFINED", "EXISTVAR", "GETVAR", "GETVARS", "SETVAR":
		return true
	case "ENUMFUNCBEGINSWITH", "ENUMFUNCENDSWITH", "ENUMFUNCWITH":
		return true
	case "ENUMVARBEGINSWITH", "ENUMVARENDSWITH", "ENUMVARWITH":
		return true
	case "ENUMMACROBEGINSWITH", "ENUMMACROENDSWITH", "ENUMMACROWITH":
		return true
	case "EXISTFUNCTION":
		return true
	default:
		return false
	}
}

func callExprRawArg(args []Value, missing []bool) string {
	rawArgs := make([]string, 0, len(args))
	for i, av := range args {
		if i < len(missing) && missing[i] {
			rawArgs = append(rawArgs, "")
			continue
		}
		if av.Kind() == StringKind {
			rawArgs = append(rawArgs, strconv.Quote(av.String()))
		} else {
			rawArgs = append(rawArgs, strconv.FormatInt(av.Int64(), 10))
		}
	}
	return strings.Join(rawArgs, ",")
}

func callExprExprArg(args []ast.Expr) string {
	raw := make([]string, 0, len(args))
	for _, a := range args {
		if _, ok := a.(ast.EmptyLit); ok {
			raw = append(raw, "")
			continue
		}
		raw = append(raw, exprToSource(a))
	}
	return strings.Join(raw, ",")
}

func exprToSource(e ast.Expr) string {
	switch ex := e.(type) {
	case ast.IntLit:
		return strconv.FormatInt(ex.Value, 10)
	case ast.StringLit:
		return strconv.Quote(ex.Value)
	case ast.VarRef:
		var b strings.Builder
		b.WriteString(strings.ToUpper(ex.Name))
		for _, idx := range ex.Index {
			b.WriteString(":")
			b.WriteString(exprToSource(idx))
		}
		return b.String()
	case ast.UnaryExpr:
		return ex.Op + "(" + exprToSource(ex.Expr) + ")"
	case ast.BinaryExpr:
		return "(" + exprToSource(ex.Left) + " " + ex.Op + " " + exprToSource(ex.Right) + ")"
	case ast.TernaryExpr:
		return "(" + exprToSource(ex.Cond) + " ? " + exprToSource(ex.True) + " # " + exprToSource(ex.False) + ")"
	case ast.CallExpr:
		return strings.ToUpper(ex.Name) + "(" + callExprExprArg(ex.Args) + ")"
	case ast.IncDecExpr:
		name := exprToSource(ex.Target)
		if ex.Post {
			return name + ex.Op
		}
		return ex.Op + name
	case ast.EmptyLit:
		return ""
	default:
		return ""
	}
}

func evalBinary(op string, left, right Value) (Value, error) {
	switch op {
	case "+":
		if left.Kind() == StringKind || right.Kind() == StringKind {
			return Str(left.String() + right.String()), nil
		}
		return Int(left.Int64() + right.Int64()), nil
	case "-":
		return Int(left.Int64() - right.Int64()), nil
	case "*":
		if left.Kind() == StringKind && right.Kind() != StringKind {
			n := right.Int64()
			if n <= 0 {
				return Str(""), nil
			}
			return Str(strings.Repeat(left.String(), int(n))), nil
		}
		if right.Kind() == StringKind && left.Kind() != StringKind {
			n := left.Int64()
			if n <= 0 {
				return Str(""), nil
			}
			return Str(strings.Repeat(right.String(), int(n))), nil
		}
		return Int(left.Int64() * right.Int64()), nil
	case "/":
		if right.Int64() == 0 {
			// Emulate permissive gameplay behavior for broken scripts/data:
			// treat x/0 as x/1 instead of aborting execution.
			return Int(left.Int64()), nil
		}
		return Int(left.Int64() / right.Int64()), nil
	case "%":
		if right.Int64() == 0 {
			// Keep modulo consistent with the x/1 fallback above.
			return Int(0), nil
		}
		return Int(left.Int64() % right.Int64()), nil
	case "<<":
		return Int(left.Int64() << right.Int64()), nil
	case ">>":
		return Int(left.Int64() >> right.Int64()), nil
	case "&":
		return Int(left.Int64() & right.Int64()), nil
	case "|":
		return Int(left.Int64() | right.Int64()), nil
	case "^":
		return Int(left.Int64() ^ right.Int64()), nil
	case "^^":
		if left.Truthy() != right.Truthy() {
			return Int(1), nil
		}
		return Int(0), nil
	case "==":
		if left.Kind() == StringKind || right.Kind() == StringKind {
			if left.String() == right.String() {
				return Int(1), nil
			}
			return Int(0), nil
		}
		if left.Int64() == right.Int64() {
			return Int(1), nil
		}
		return Int(0), nil
	case "!=":
		if left.Kind() == StringKind || right.Kind() == StringKind {
			if left.String() != right.String() {
				return Int(1), nil
			}
			return Int(0), nil
		}
		if left.Int64() != right.Int64() {
			return Int(1), nil
		}
		return Int(0), nil
	case "<":
		if left.Int64() < right.Int64() {
			return Int(1), nil
		}
		return Int(0), nil
	case "<=":
		if left.Int64() <= right.Int64() {
			return Int(1), nil
		}
		return Int(0), nil
	case ">":
		if left.Int64() > right.Int64() {
			return Int(1), nil
		}
		return Int(0), nil
	case ">=":
		if left.Int64() >= right.Int64() {
			return Int(1), nil
		}
		return Int(0), nil
	case "&&":
		if left.Truthy() && right.Truthy() {
			return Int(1), nil
		}
		return Int(0), nil
	case "!&":
		if !(left.Truthy() && right.Truthy()) {
			return Int(1), nil
		}
		return Int(0), nil
	case "||":
		if left.Truthy() || right.Truthy() {
			return Int(1), nil
		}
		return Int(0), nil
	case "!|":
		if !(left.Truthy() || right.Truthy()) {
			return Int(1), nil
		}
		return Int(0), nil
	default:
		return Value{}, fmt.Errorf("unsupported binary operator %q", op)
	}
}

func evalAssignBinary(op string, left, right Value) (Value, error) {
	switch op {
	case "+=":
		return evalBinary("+", left, right)
	case "-=":
		return evalBinary("-", left, right)
	case "*=":
		return evalBinary("*", left, right)
	case "/=":
		return evalBinary("/", left, right)
	case "%=":
		return evalBinary("%", left, right)
	case "&=":
		return evalBinary("&", left, right)
	case "|=":
		return evalBinary("|", left, right)
	case "^=":
		return evalBinary("^", left, right)
	case "<<=":
		return evalBinary("<<", left, right)
	case ">>=":
		return evalBinary(">>", left, right)
	default:
		return Value{}, fmt.Errorf("unsupported assignment operator %q", op)
	}
}
