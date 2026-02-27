package parser

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/gosuda/erago/ast"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokInt
	tokString
	tokIdent
	tokLParen
	tokRParen
	tokComma
	tokColon
	tokQuestion
	tokHash
	tokOp
)

type token struct {
	kind tokenKind
	lit  string
}

func ParseExpr(raw string) (ast.Expr, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 && strings.HasPrefix(raw, "%") && strings.HasSuffix(raw, "%") {
		inner := strings.TrimSpace(raw[1 : len(raw)-1])
		if inner != "" {
			raw = inner
		}
	}
	raw = normalizeExprSyntax(raw)
	toks, err := tokenizeExpr(raw)
	if err != nil {
		return nil, err
	}
	p := &exprParser{tokens: toks}
	expr, err := p.parse(1)
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, fmt.Errorf("unexpected token %q", p.peek().lit)
	}
	return expr, nil
}

func normalizeExprSyntax(raw string) string {
	if raw == "" {
		return raw
	}
	rs := []rune(raw)
	var b strings.Builder
	inString := false
	verbatim := false
	escape := false
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if inString {
			b.WriteRune(r)
			if verbatim {
				if r == '"' {
					inString = false
					verbatim = false
				}
				continue
			}
			if escape {
				escape = false
				continue
			}
			if r == '\\' {
				escape = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}

		if r == '@' && i+1 < len(rs) && rs[i+1] == '"' {
			b.WriteRune('@')
			b.WriteRune('"')
			i++
			inString = true
			verbatim = true
			continue
		}
		if r == '"' {
			b.WriteRune('"')
			inString = true
			verbatim = false
			continue
		}
		if r == '　' {
			b.WriteRune(' ')
			continue
		}
		if r >= 0xFF01 && r <= 0xFF5E {
			b.WriteRune(r - 0xFEE0)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

type exprParser struct {
	tokens []token
	pos    int
	depth  int
}

func (p *exprParser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{kind: tokEOF}
	}
	return p.tokens[p.pos]
}

func (p *exprParser) next() token {
	t := p.peek()
	p.pos++
	return t
}

func (p *exprParser) parse(minPrec int) (ast.Expr, error) {
	p.depth++
	if p.depth > 256 {
		return nil, fmt.Errorf("expression nesting too deep near token %q", p.peek().lit)
	}
	defer func() { p.depth-- }()

	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}
	for {
		tok := p.peek()
		if tok.kind != tokOp {
			break
		}
		prec := opPrecedence(tok.lit)
		if prec < minPrec {
			break
		}
		op := p.next().lit
		right, err := p.parse(prec + 1)
		if err != nil {
			return nil, err
		}
		left = ast.BinaryExpr{Op: op, Left: left, Right: right}
	}
	if minPrec <= 1 && p.peek().kind == tokQuestion {
		p.next()
		onTrue, err := p.parse(1)
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokHash {
			return nil, fmt.Errorf("missing # in ternary expression")
		}
		p.next()
		onFalse, err := p.parse(1)
		if err != nil {
			return nil, err
		}
		left = ast.TernaryExpr{Cond: left, True: onTrue, False: onFalse}
	}
	return left, nil
}

func (p *exprParser) parsePrefix() (ast.Expr, error) {
	t := p.next()
	switch t.kind {
	case tokInt:
		v, err := strconv.ParseInt(t.lit, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %q", t.lit)
		}
		return ast.IntLit{Value: v}, nil
	case tokString:
		return ast.StringLit{Value: t.lit}, nil
	case tokIdent:
		if p.peek().kind == tokLParen {
			p.next()
			args := []ast.Expr{}
			if p.peek().kind != tokRParen {
				for {
					if p.peek().kind == tokComma {
						args = append(args, ast.EmptyLit{})
						p.next()
						continue
					}
					if p.peek().kind == tokRParen {
						break
					}
					e, err := p.parse(1)
					if err != nil {
						return nil, err
					}
					args = append(args, e)
					if p.peek().kind == tokComma {
						p.next()
						continue
					}
					break
				}
			}
			if p.peek().kind != tokRParen {
				return nil, fmt.Errorf("missing ) in call expression")
			}
			p.next()
			return ast.CallExpr{Name: strings.ToUpper(t.lit), Args: args}, nil
		}
		ref := ast.VarRef{Name: strings.ToUpper(t.lit), Index: nil}
		for p.peek().kind == tokColon {
			p.next()
			idxExpr, err := p.parse(11)
			if err != nil {
				return nil, fmt.Errorf("invalid index expression for %s: %w", ref.Name, err)
			}
			ref.Index = append(ref.Index, idxExpr)
		}
		if p.peek().kind == tokOp && (p.peek().lit == "++" || p.peek().lit == "--") {
			op := p.next().lit
			return ast.IncDecExpr{Target: ref, Op: op, Post: true}, nil
		}
		return ref, nil
	case tokLParen:
		e, err := p.parse(1)
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			return nil, fmt.Errorf("missing )")
		}
		p.next()
		return e, nil
	case tokOp:
		if t.lit == "+" || t.lit == "-" || t.lit == "!" || t.lit == "~" {
			right, err := p.parse(11)
			if err != nil {
				return nil, err
			}
			return ast.UnaryExpr{Op: t.lit, Expr: right}, nil
		}
		if t.lit == "++" || t.lit == "--" {
			right, err := p.parse(11)
			if err != nil {
				return nil, err
			}
			ref, ok := right.(ast.VarRef)
			if !ok {
				return nil, fmt.Errorf("%s requires variable reference", t.lit)
			}
			return ast.IncDecExpr{Target: ref, Op: t.lit, Post: false}, nil
		}
	}
	return nil, fmt.Errorf("unexpected token %q", t.lit)
}

func opPrecedence(op string) int {
	switch op {
	case "!|":
		return 1
	case "||":
		return 1
	case "!&":
		return 2
	case "&&":
		return 2
	case "|":
		return 3
	case "^", "^^":
		return 4
	case "&":
		return 5
	case "==", "!=":
		return 6
	case "<", "<=", ">", ">=":
		return 7
	case "<<", ">>":
		return 8
	case "+", "-":
		return 9
	case "*", "/", "%":
		return 10
	default:
		return 0
	}
}

func tokenizeExpr(raw string) ([]token, error) {
	toks := make([]token, 0, len(raw)/2)
	r := []rune(strings.TrimSpace(raw))
	for i := 0; i < len(r); {
		ch := r[i]
		if unicode.IsSpace(ch) {
			i++
			continue
		}
		if unicode.IsDigit(ch) {
			if ch == '0' && i+2 < len(r) && (r[i+1] == 'b' || r[i+1] == 'B') && (r[i+2] == '0' || r[i+2] == '1') {
				j := i + 3
				for j < len(r) && (r[j] == '0' || r[j] == '1') {
					j++
				}
				v, err := strconv.ParseInt(string(r[i+2:j]), 2, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid binary integer %q", string(r[i:j]))
				}
				toks = append(toks, token{kind: tokInt, lit: strconv.FormatInt(v, 10)})
				i = j
				continue
			}
			if ch == '0' && i+2 < len(r) && (r[i+1] == 'x' || r[i+1] == 'X') && isHexDigit(r[i+2]) {
				j := i + 3
				for j < len(r) && isHexDigit(r[j]) {
					j++
				}
				v, err := strconv.ParseInt(string(r[i+2:j]), 16, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid hex integer %q", string(r[i:j]))
				}
				toks = append(toks, token{kind: tokInt, lit: strconv.FormatInt(v, 10)})
				i = j
				continue
			}
			j := i + 1
			for j < len(r) && unicode.IsDigit(r[j]) {
				j++
			}
			if j+1 < len(r) && (r[j] == 'p' || r[j] == 'P') && unicode.IsDigit(r[j+1]) {
				k := j + 2
				for k < len(r) && unicode.IsDigit(r[k]) {
					k++
				}
				base, err := strconv.ParseInt(string(r[i:j]), 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid integer %q", string(r[i:k]))
				}
				shift, err := strconv.ParseInt(string(r[j+1:k]), 10, 64)
				if err != nil || shift < 0 || shift > 62 {
					return nil, fmt.Errorf("invalid bit literal %q", string(r[i:k]))
				}
				toks = append(toks, token{kind: tokInt, lit: strconv.FormatInt(base<<shift, 10)})
				i = k
				continue
			}
			toks = append(toks, token{kind: tokInt, lit: string(r[i:j])})
			i = j
			continue
		}
		if ch == '"' {
			j := i + 1
			escape := false
			for j < len(r) {
				if escape {
					escape = false
					j++
					continue
				}
				if r[j] == '\\' {
					escape = true
					j++
					continue
				}
				if r[j] == '"' {
					break
				}
				j++
			}
			if j >= len(r) || r[j] != '"' {
				return nil, fmt.Errorf("unterminated string")
			}
			v, ok := unquoteString(string(r[i : j+1]))
			if !ok {
				return nil, fmt.Errorf("invalid string literal")
			}
			toks = append(toks, token{kind: tokString, lit: v})
			i = j + 1
			continue
		}
		if (ch == 'b' || ch == 'B') && i+1 < len(r) && (r[i+1] == '0' || r[i+1] == '1') {
			j := i + 2
			for j < len(r) && (r[j] == '0' || r[j] == '1') {
				j++
			}
			v, err := strconv.ParseInt(string(r[i+1:j]), 2, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid binary integer %q", string(r[i:j]))
			}
			toks = append(toks, token{kind: tokInt, lit: strconv.FormatInt(v, 10)})
			i = j
			continue
		}
		if ch == '@' && i+1 < len(r) && r[i+1] == '"' {
			j := i + 2
			inPercent := false
			inExprString := false
			exprVerbatim := false
			exprEscape := false
			for j < len(r) {
				c := r[j]
				if inPercent {
					if inExprString {
						if exprVerbatim {
							if c == '"' {
								inExprString = false
								exprVerbatim = false
							}
							j++
							continue
						}
						if exprEscape {
							exprEscape = false
							j++
							continue
						}
						if c == '\\' {
							exprEscape = true
							j++
							continue
						}
						if c == '"' {
							inExprString = false
							j++
							continue
						}
						j++
						continue
					}
					if c == '@' && j+1 < len(r) && r[j+1] == '"' {
						inExprString = true
						exprVerbatim = true
						j += 2
						continue
					}
					if c == '"' {
						inExprString = true
						exprVerbatim = false
						j++
						continue
					}
					if c == '%' {
						inPercent = false
						j++
						continue
					}
					j++
					continue
				}
				if c == '%' {
					inPercent = true
					j++
					continue
				}
				if c == '"' {
					break
				}
				j++
			}
			if j >= len(r) || r[j] != '"' {
				return nil, fmt.Errorf("unterminated verbatim string")
			}
			toks = append(toks, token{kind: tokString, lit: string(r[i+2 : j])})
			i = j + 1
			continue
		}
		if isIdentStart(ch) {
			j := i + 1
			for j < len(r) && isIdentPart(r[j]) {
				j++
			}
			toks = append(toks, token{kind: tokIdent, lit: string(r[i:j])})
			i = j
			continue
		}
		if ch == '(' {
			toks = append(toks, token{kind: tokLParen, lit: "("})
			i++
			continue
		}
		if ch == ')' {
			toks = append(toks, token{kind: tokRParen, lit: ")"})
			i++
			continue
		}
		if ch == ',' {
			toks = append(toks, token{kind: tokComma, lit: ","})
			i++
			continue
		}
		if ch == ':' {
			toks = append(toks, token{kind: tokColon, lit: ":"})
			i++
			continue
		}
		if ch == '?' {
			toks = append(toks, token{kind: tokQuestion, lit: "?"})
			i++
			continue
		}
		if ch == '#' {
			toks = append(toks, token{kind: tokHash, lit: "#"})
			i++
			continue
		}
		if i+1 < len(r) {
			two := string(r[i : i+2])
			switch two {
			case "<=", ">=", "==", "!=", "&&", "||", "<<", ">>", "!|", "!&", "++", "--", "^^":
				toks = append(toks, token{kind: tokOp, lit: two})
				i += 2
				continue
			}
		}
		switch ch {
		case '+', '-', '*', '/', '%', '<', '>', '&', '|', '^', '!', '~':
			toks = append(toks, token{kind: tokOp, lit: string(ch)})
			i++
		default:
			return nil, fmt.Errorf("unexpected character %q", ch)
		}
	}
	toks = append(toks, token{kind: tokEOF})
	return toks, nil
}

func isIdentStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isIdentPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '・' || r == '·'
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func ParseExprList(raw string) ([]ast.Expr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := splitTopLevel(raw, ',')
	out := make([]ast.Expr, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			out = append(out, ast.EmptyLit{})
			continue
		}
		e, err := ParseExpr(p)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
