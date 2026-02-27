package parser

import "strings"

type Line struct {
	File    string
	Number  int
	Content string
}

func normalize(raw string) string {
	if after, ok := strings.CutPrefix(raw, "\uFEFF"); ok {
		return after
	}
	return raw
}

func toLines(file, raw string) []Line {
	norm := normalize(raw)
	norm = strings.ReplaceAll(norm, "\r\n", "\n")
	norm = strings.ReplaceAll(norm, "\r", "\n")
	parts := strings.Split(norm, "\n")
	out := make([]Line, 0, len(parts))
	for i, p := range parts {
		out = append(out, Line{File: file, Number: i + 1, Content: p})
	}
	return out
}

func preprocess(lines []Line, macros map[string]struct{}) []Line {
	out := make([]Line, 0, len(lines))
	for _, l := range lines {
		line := l
		line.Content = stripComment(line.Content)
		line.Content = strings.TrimSpace(line.Content)
		if line.Content == "" {
			continue
		}
		out = append(out, line)
	}

	out = stripRange(out, "[SKIPSTART]", "[SKIPEND]")
	out = stripRange(out, "[IF_DEBUG]", "[ENDIF]")

	processed := make([]Line, 0, len(out))
	for i := 0; i < len(out); i++ {
		line := out[i]
		if strings.HasPrefix(line.Content, "[IF ") && strings.HasSuffix(line.Content, "]") {
			name := strings.TrimSpace(line.Content[len("[IF ") : len(line.Content)-1])
			end := i + 1
			for end < len(out) && !strings.EqualFold(out[end].Content, "[ENDIF]") {
				end++
			}
			if end >= len(out) {
				break
			}
			if _, ok := macros[strings.ToUpper(name)]; ok {
				processed = append(processed, out[i+1:end]...)
			}
			i = end
			continue
		}
		processed = append(processed, line)
	}

	return concatBraceLines(processed)
}

func stripRange(lines []Line, start, end string) []Line {
	out := make([]Line, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if strings.EqualFold(lines[i].Content, start) {
			j := i + 1
			for j < len(lines) && !strings.EqualFold(lines[j].Content, end) {
				j++
			}
			i = j
			continue
		}
		out = append(out, lines[i])
	}
	return out
}

func concatBraceLines(lines []Line) []Line {
	out := make([]Line, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if lines[i].Content != "{" {
			out = append(out, lines[i])
			continue
		}
		j := i + 1
		for j < len(lines) && lines[j].Content != "}" {
			j++
		}
		if j >= len(lines) || j == i+1 {
			i = j
			continue
		}
		parts := make([]string, 0, j-i-1)
		for _, l := range lines[i+1 : j] {
			parts = append(parts, l.Content)
		}
		out = append(out, Line{File: lines[i+1].File, Number: lines[i+1].Number, Content: strings.Join(parts, "")})
		i = j
	}
	return out
}

func stripComment(raw string) string {
	if raw == "" {
		return raw
	}
	rs := []rune(raw)
	out := make([]rune, 0, len(rs))
	inString := false
	verbatim := false
	escape := false
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if !inString {
			if r == '@' && i+1 < len(rs) && rs[i+1] == '"' {
				inString = true
				verbatim = true
				out = append(out, '@', '"')
				i++
				continue
			}
			if r == '"' {
				inString = true
				verbatim = false
				escape = false
				out = append(out, r)
				continue
			}
			if r == ';' {
				break
			}
			out = append(out, r)
			continue
		}

		out = append(out, r)
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
	}
	return string(out)
}
