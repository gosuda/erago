package eruntime

import (
	"strings"

	"github.com/gosuda/erago/parser"
)

func (vm *VM) evalPrintForm(arg string) (string, error) {
	tmpl := decodeCommandCharSeq(arg)
	out, err := vm.evalPercentPlaceholders(tmpl)
	if err != nil {
		return "", err
	}
	out, err = vm.evalBracePlaceholders(out)
	if err != nil {
		return "", err
	}
	return out, nil
}

func decodeCommandCharSeq(raw string) string {
	if raw == "" {
		return ""
	}
	b := strings.Builder{}
	escape := false
	for _, r := range raw {
		if escape {
			switch r {
			case 's':
				b.WriteRune(' ')
			case 'S':
				b.WriteRune('　')
			case 't':
				b.WriteRune('\t')
			case 'n':
				b.WriteRune('\n')
			default:
				b.WriteRune(r)
			}
			escape = false
			continue
		}
		if r == '\\' {
			escape = true
			continue
		}
		b.WriteRune(r)
	}
	if escape {
		b.WriteRune('\\')
	}
	return b.String()
}

func (vm *VM) evalPercentPlaceholders(tmpl string) (string, error) {
	b := strings.Builder{}
	for i := 0; i < len(tmpl); {
		if tmpl[i] != '%' {
			b.WriteByte(tmpl[i])
			i++
			continue
		}
		j := i + 1
		for j < len(tmpl) && tmpl[j] != '%' {
			j++
		}
		if j >= len(tmpl) {
			b.WriteString(tmpl[i:])
			break
		}
		exprRaw := strings.TrimSpace(tmpl[i+1 : j])
		if exprRaw == "" {
			b.WriteString("%%")
			i = j + 1
			continue
		}
		expr, err := parser.ParseExpr(exprRaw)
		if err != nil {
			b.WriteString(tmpl[i : j+1])
			i = j + 1
			continue
		}
		v, err := vm.evalExpr(expr)
		if err != nil {
			return "", err
		}
		b.WriteString(v.String())
		i = j + 1
	}
	return b.String(), nil
}

func (vm *VM) evalBracePlaceholders(tmpl string) (string, error) {
	b := strings.Builder{}
	for i := 0; i < len(tmpl); {
		if tmpl[i] != '{' {
			b.WriteByte(tmpl[i])
			i++
			continue
		}
		j := i + 1
		depth := 1
		for j < len(tmpl) && depth > 0 {
			if tmpl[j] == '{' {
				depth++
			} else if tmpl[j] == '}' {
				depth--
			}
			if depth == 0 {
				break
			}
			j++
		}
		if j >= len(tmpl) || depth != 0 {
			b.WriteString(tmpl[i:])
			break
		}
		exprRaw := strings.TrimSpace(tmpl[i+1 : j])
		if exprRaw == "" {
			b.WriteString("{}")
			i = j + 1
			continue
		}
		expr, err := parser.ParseExpr(exprRaw)
		if err != nil {
			b.WriteString(tmpl[i : j+1])
			i = j + 1
			continue
		}
		v, err := vm.evalExpr(expr)
		if err != nil {
			return "", err
		}
		b.WriteString(v.String())
		i = j + 1
	}
	return b.String(), nil
}
