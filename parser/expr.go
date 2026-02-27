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
	tokOp
)

type token struct {
	kind tokenKind
	lit  string
}

func ParseExpr(raw string) (ast.Expr, error) {
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

type exprParser struct {
	tokens []token
	pos    int
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
			return nil, fmt.Errorf("inline call in expression is not supported yet: %s(...)", t.lit)
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
	}
	return nil, fmt.Errorf("unexpected token %q", t.lit)
}

func opPrecedence(op string) int {
	switch op {
	case "||":
		return 1
	case "&&":
		return 2
	case "|":
		return 3
	case "^":
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
			j := i + 1
			for j < len(r) && unicode.IsDigit(r[j]) {
				j++
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
		if i+1 < len(r) {
			two := string(r[i : i+2])
			switch two {
			case "<=", ">=", "==", "!=", "&&", "||", "<<", ">>":
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
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func ParseExprList(raw string) ([]ast.Expr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := splitTopLevel(raw, ',')
	out := make([]ast.Expr, 0, len(parts))
	for _, p := range parts {
		if p == "" {
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
