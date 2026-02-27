package eruntime

import (
	"strconv"
	"strings"

	"github.com/gosuda/erago/ast"
	"github.com/gosuda/erago/parser"
)

func (vm *VM) evalPrintForm(arg string) (string, error) {
	tmpl := decodeCommandCharSeq(arg)
	return vm.expandFormTemplate(tmpl)
}

func (vm *VM) expandFormTemplate(tmpl string) (string, error) {
	out := tmpl
	for i := 0; i < 8; i++ {
		prev := out
		t, err := vm.evalPercentPlaceholders(out)
		if err != nil {
			return "", err
		}
		out = t
		t, err = vm.evalBracePlaceholders(out)
		if err != nil {
			return "", err
		}
		out = t
		t, err = vm.evalAtPlaceholders(out)
		if err != nil {
			return "", err
		}
		out = t
		if out == prev {
			break
		}
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
		j, ok := findPercentPlaceholderEnd(tmpl, i+1)
		if !ok {
			b.WriteString(tmpl[i:])
			break
		}
		exprRaw := strings.TrimSpace(tmpl[i+1 : j])
		if exprRaw == "" {
			b.WriteString("%%")
			i = j + 1
			continue
		}
		repl, ok, err := vm.evalPercentPlaceholderExpr(exprRaw)
		if err != nil {
			return "", err
		}
		if !ok {
			b.WriteString(tmpl[i : j+1])
			i = j + 1
			continue
		}
		b.WriteString(repl)
		i = j + 1
	}
	return b.String(), nil
}

func (vm *VM) evalPercentPlaceholderExpr(raw string) (string, bool, error) {
	if nested, ok := unwrapNestedAt(raw); ok {
		repl, handled, err := vm.evalAtPlaceholderExpr(nested)
		if err != nil {
			return "", false, err
		}
		if handled {
			return repl, true, nil
		}
	}

	expr, err := parser.ParseExpr(raw)
	if err == nil {
		v, err := vm.evalExpr(expr)
		if err != nil {
			return "", false, err
		}
		return v.String(), true, nil
	}

	parts := splitTopLevelRuntime(raw, ',')
	if len(parts) < 2 {
		if text, ok, err := vm.evalFormStringExpr(raw); err != nil {
			return "", false, err
		} else if ok {
			return text, true, nil
		}
		return "", false, nil
	}
	baseRaw := strings.TrimSpace(parts[0])
	widthRaw := strings.TrimSpace(parts[1])
	if baseRaw == "" || widthRaw == "" {
		return "", false, nil
	}
	baseExpr, err := parser.ParseExpr(baseRaw)
	baseText := ""
	if err == nil {
		baseVal, err := vm.evalExpr(baseExpr)
		if err != nil {
			return "", false, err
		}
		baseText = baseVal.String()
	} else {
		text, ok, err := vm.evalFormStringExpr(baseRaw)
		if err != nil {
			return "", false, err
		}
		if !ok {
			return "", false, nil
		}
		baseText = text
	}
	widthVal, err := vm.evalLooseExpr(widthRaw)
	if err != nil {
		n, convErr := strconv.ParseInt(widthRaw, 10, 64)
		if convErr != nil {
			return "", false, nil
		}
		widthVal = Int(n)
	}
	align := "RIGHT"
	if len(parts) >= 3 {
		alignRaw := strings.TrimSpace(parts[2])
		if alignRaw != "" {
			if av, err := vm.evalLooseExpr(alignRaw); err == nil {
				align = strings.ToUpper(strings.TrimSpace(av.String()))
			} else {
				align = strings.ToUpper(strings.Trim(alignRaw, "\""))
			}
		}
	}
	return formatPrintField(baseText, int(widthVal.Int64()), align), true, nil
}

func formatPrintField(text string, width int, align string) string {
	if width < 0 {
		width = -width
	}
	if width == 0 {
		return text
	}
	rlen := len([]rune(text))
	if rlen >= width {
		return text
	}
	pad := strings.Repeat(" ", width-rlen)
	switch align {
	case "LEFT":
		return text + pad
	case "CENTER", "MIDDLE":
		left := (width - rlen) / 2
		right := width - rlen - left
		return strings.Repeat(" ", left) + text + strings.Repeat(" ", right)
	default:
		return pad + text
	}
}

func (vm *VM) evalFormStringExpr(raw string) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true, nil
	}
	parts := splitTopLevelRuntime(raw, '+')
	if len(parts) <= 1 {
		text, ok, err := vm.evalFormStringTerm(raw)
		if err != nil {
			return "", false, err
		}
		return text, ok, nil
	}
	var b strings.Builder
	for _, p := range parts {
		text, ok, err := vm.evalFormStringTerm(p)
		if err != nil {
			return "", false, err
		}
		if !ok {
			return "", false, nil
		}
		b.WriteString(text)
	}
	return b.String(), true, nil
}

func (vm *VM) evalFormStringTerm(term string) (string, bool, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return "", true, nil
	}
	if strings.HasPrefix(term, "@") && strings.HasSuffix(term, "@") && len(term) >= 2 {
		inner := strings.TrimSpace(term[1 : len(term)-1])
		if inner == "" {
			return "", true, nil
		}
		text, ok, err := vm.evalAtPlaceholderExpr(inner)
		if err != nil {
			return "", false, err
		}
		if ok {
			return text, true, nil
		}
	}
	if uq, ok := tryUnquoteCommandString(term); ok {
		text, err := vm.expandDecodedTemplate(uq)
		if err != nil {
			return "", false, err
		}
		return text, true, nil
	}
	if expr, err := parser.ParseExpr(term); err == nil {
		v, err := vm.evalExpr(expr)
		if err != nil {
			return "", false, err
		}
		return v.String(), true, nil
	}
	text, err := vm.evalAtBranch(term)
	if err != nil {
		return "", false, err
	}
	return text, true, nil
}

func (vm *VM) evalBracePlaceholders(tmpl string) (string, error) {
	b := strings.Builder{}
	for i := 0; i < len(tmpl); {
		if tmpl[i] != '{' {
			b.WriteByte(tmpl[i])
			i++
			continue
		}
		j, ok := findBracePlaceholderEnd(tmpl, i+1)
		if !ok {
			b.WriteString(tmpl[i:])
			break
		}
		exprRaw := strings.TrimSpace(tmpl[i+1 : j])
		if exprRaw == "" {
			b.WriteString("{}")
			i = j + 1
			continue
		}
		repl, ok, err := vm.evalBracePlaceholderExpr(exprRaw)
		if err != nil {
			return "", err
		}
		if !ok {
			b.WriteString(tmpl[i : j+1])
			i = j + 1
			continue
		}
		b.WriteString(repl)
		i = j + 1
	}
	return b.String(), nil
}

func (vm *VM) evalBracePlaceholderExpr(raw string) (string, bool, error) {
	expr, err := parser.ParseExpr(raw)
	if err == nil {
		v, err := vm.evalExpr(expr)
		if err != nil {
			return "", false, err
		}
		return v.String(), true, nil
	}
	parts := splitTopLevelRuntime(raw, ',')
	if len(parts) < 2 {
		return "", false, nil
	}
	baseRaw := strings.TrimSpace(parts[0])
	widthRaw := strings.TrimSpace(parts[1])
	if baseRaw == "" || widthRaw == "" {
		return "", false, nil
	}
	baseExpr, err := parser.ParseExpr(baseRaw)
	if err != nil {
		return "", false, nil
	}
	baseVal, err := vm.evalExpr(baseExpr)
	if err != nil {
		return "", false, err
	}
	widthVal, err := vm.evalLooseExpr(widthRaw)
	if err != nil {
		n, convErr := strconv.ParseInt(widthRaw, 10, 64)
		if convErr != nil {
			return "", false, nil
		}
		widthVal = Int(n)
	}
	align := "RIGHT"
	if len(parts) >= 3 {
		alignRaw := strings.TrimSpace(parts[2])
		if alignRaw != "" {
			if av, err := vm.evalLooseExpr(alignRaw); err == nil {
				align = strings.ToUpper(strings.TrimSpace(av.String()))
			} else {
				align = strings.ToUpper(strings.Trim(alignRaw, "\""))
			}
		}
	}
	return formatPrintField(baseVal.String(), int(widthVal.Int64()), align), true, nil
}

func (vm *VM) evalAtPlaceholders(tmpl string) (string, error) {
	b := strings.Builder{}
	for i := 0; i < len(tmpl); {
		if tmpl[i] != '@' {
			b.WriteByte(tmpl[i])
			i++
			continue
		}
		j, ok := findAtPlaceholderEnd(tmpl, i+1)
		if !ok {
			b.WriteByte(tmpl[i])
			i++
			continue
		}
		exprRaw := strings.TrimSpace(tmpl[i+1 : j])
		if exprRaw == "" {
			b.WriteString(tmpl[i : j+1])
			i = j + 1
			continue
		}
		repl, handled, err := vm.evalAtPlaceholderExpr(exprRaw)
		if err != nil {
			return "", err
		}
		if !handled {
			b.WriteString(tmpl[i : j+1])
			i = j + 1
			continue
		}
		b.WriteString(repl)
		i = j + 1
	}
	return b.String(), nil
}

func (vm *VM) evalAtPlaceholderExpr(raw string) (string, bool, error) {
	condRaw, tRaw, fRaw, ok := splitTopLevelTernary(raw)
	if ok {
		cond, err := vm.evalLooseExpr(condRaw)
		if err != nil {
			return "", false, err
		}
		branch := fRaw
		if cond.Truthy() {
			branch = tRaw
		}
		text, err := vm.evalAtBranch(strings.TrimSpace(branch))
		if err != nil {
			return "", false, err
		}
		return text, true, nil
	}

	expr, err := parser.ParseExpr(raw)
	if err != nil {
		return "", false, nil
	}
	if ref, ok := expr.(ast.VarRef); ok && len(ref.Index) == 0 && !vm.symbolExists(ref.Name) {
		return raw, true, nil
	}
	v, err := vm.evalExpr(expr)
	if err != nil {
		return "", false, err
	}
	return v.String(), true, nil
}

func (vm *VM) evalAtBranch(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if inner, ok := unwrapFullPercent(raw); ok {
		v, err := vm.evalLooseExpr(inner)
		if err != nil {
			return "", err
		}
		return v.String(), nil
	}
	if expr, err := parser.ParseExpr(raw); err == nil {
		if ref, ok := expr.(ast.VarRef); ok && len(ref.Index) == 0 && !vm.symbolExists(ref.Name) {
			return raw, nil
		}
		v, err := vm.evalExpr(expr)
		if err != nil {
			return "", err
		}
		return v.String(), nil
	}
	t, err := vm.expandFormTemplate(raw)
	if err != nil {
		return "", err
	}
	return t, nil
}

func unwrapFullPercent(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '%' {
		return "", false
	}
	end, ok := findPercentPlaceholderEnd(raw, 1)
	if !ok || end != len(raw)-1 {
		return "", false
	}
	return strings.TrimSpace(raw[1 : len(raw)-1]), true
}

func splitTopLevelTernary(raw string) (cond, onTrue, onFalse string, ok bool) {
	depth := 0
	inString := false
	verbatim := false
	escape := false
	q := -1
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			if verbatim {
				if ch == '"' {
					inString = false
					verbatim = false
				}
				continue
			}
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '@' && i+1 < len(raw) && raw[i+1] == '"' {
			inString = true
			verbatim = true
			i++
			continue
		}
		if ch == '"' {
			inString = true
			verbatim = false
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '?':
			if depth == 0 && q < 0 {
				q = i
			}
		case '#':
			if depth == 0 && q >= 0 {
				return strings.TrimSpace(raw[:q]), strings.TrimSpace(raw[q+1 : i]), strings.TrimSpace(raw[i+1:]), true
			}
		}
	}
	return "", "", "", false
}

func unwrapNestedAt(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '@' || raw[len(raw)-1] != '@' {
		return "", false
	}
	inner := strings.TrimSpace(raw[1 : len(raw)-1])
	if inner == "" {
		return "", false
	}
	return inner, true
}

func findPercentPlaceholderEnd(s string, start int) (int, bool) {
	inString := false
	verbatim := false
	escape := false
	verbInPercent := false
	verbInExprString := false
	verbExprVerbatim := false
	verbExprEscape := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			if verbatim {
				if verbInPercent {
					if verbInExprString {
						if verbExprVerbatim {
							if ch == '"' {
								verbInExprString = false
								verbExprVerbatim = false
							}
							continue
						}
						if verbExprEscape {
							verbExprEscape = false
							continue
						}
						if ch == '\\' {
							verbExprEscape = true
							continue
						}
						if ch == '"' {
							verbInExprString = false
						}
						continue
					}
					if ch == '@' && i+1 < len(s) && s[i+1] == '"' {
						verbInExprString = true
						verbExprVerbatim = true
						i++
						continue
					}
					if ch == '"' {
						verbInExprString = true
						verbExprVerbatim = false
						continue
					}
					if ch == '%' {
						verbInPercent = false
					}
					continue
				}
				if ch == '%' {
					verbInPercent = true
					continue
				}
				if ch == '"' {
					inString = false
					verbatim = false
					verbInPercent = false
					verbInExprString = false
					verbExprVerbatim = false
					verbExprEscape = false
				}
				continue
			}
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '@' && i+1 < len(s) && s[i+1] == '"' {
			inString = true
			verbatim = true
			i++
			continue
		}
		if ch == '"' {
			inString = true
			verbatim = false
			continue
		}
		if ch == '%' {
			return i, true
		}
	}
	return 0, false
}

func findBracePlaceholderEnd(s string, start int) (int, bool) {
	depth := 1
	inString := false
	verbatim := false
	escape := false
	verbInPercent := false
	verbInExprString := false
	verbExprVerbatim := false
	verbExprEscape := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			if verbatim {
				if verbInPercent {
					if verbInExprString {
						if verbExprVerbatim {
							if ch == '"' {
								verbInExprString = false
								verbExprVerbatim = false
							}
							continue
						}
						if verbExprEscape {
							verbExprEscape = false
							continue
						}
						if ch == '\\' {
							verbExprEscape = true
							continue
						}
						if ch == '"' {
							verbInExprString = false
						}
						continue
					}
					if ch == '@' && i+1 < len(s) && s[i+1] == '"' {
						verbInExprString = true
						verbExprVerbatim = true
						i++
						continue
					}
					if ch == '"' {
						verbInExprString = true
						verbExprVerbatim = false
						continue
					}
					if ch == '%' {
						verbInPercent = false
					}
					continue
				}
				if ch == '%' {
					verbInPercent = true
					continue
				}
				if ch == '"' {
					inString = false
					verbatim = false
					verbInPercent = false
					verbInExprString = false
					verbExprVerbatim = false
					verbExprEscape = false
				}
				continue
			}
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '@' && i+1 < len(s) && s[i+1] == '"' {
			inString = true
			verbatim = true
			i++
			continue
		}
		if ch == '"' {
			inString = true
			verbatim = false
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func findAtPlaceholderEnd(s string, start int) (int, bool) {
	inString := false
	verbatim := false
	escape := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			if verbatim {
				if ch == '"' {
					inString = false
					verbatim = false
				}
				continue
			}
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '@' && i+1 < len(s) && s[i+1] == '"' {
			inString = true
			verbatim = true
			i++
			continue
		}
		if ch == '"' {
			inString = true
			verbatim = false
			continue
		}
		if ch == '@' {
			return i, true
		}
	}
	return 0, false
}
