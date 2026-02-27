package parser

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func splitTopLevel(raw string, sep rune) []string {
	parts := []string{}
	depth := 0
	inStr := false
	escape := false
	start := 0
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
		default:
			if r == sep && depth == 0 {
				parts = append(parts, strings.TrimSpace(raw[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(raw[start:]))
	return parts
}

func unquoteString(raw string) (string, bool) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", false
	}
	b := strings.Builder{}
	escape := false
	for _, r := range raw[1 : len(raw)-1] {
		if escape {
			switch r {
			case 'n':
				b.WriteRune('\n')
			case 'r':
				b.WriteRune('\r')
			case 't':
				b.WriteRune('\t')
			case '\\', '"':
				b.WriteRune(r)
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
		return "", false
	}
	return b.String(), true
}

func decodeCharSeq(raw string) string {
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
		if i == 0 {
			continue
		}
		if unicode.IsSpace(r) {
			return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+utf8.RuneLen(r):])
		}
		if !isIdentPart(r) {
			return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+utf8.RuneLen(r):])
		}
	}
	return raw, ""
}
