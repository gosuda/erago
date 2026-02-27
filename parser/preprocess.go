package parser

import "strings"

type Line struct {
	File    string
	Number  int
	Content string
}

func normalize(raw string) string {
	if strings.HasPrefix(raw, "\uFEFF") || strings.HasPrefix(raw, "\uFFEF") {
		return raw[1:]
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
		if idx := strings.Index(line.Content, ";"); idx >= 0 {
			line.Content = line.Content[:idx]
		}
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
