package eruntime

import (
	"encoding/csv"
	"strconv"
	"strings"
)

type CSVStore struct {
	rowsByBase map[string][][]string
	nameByBase map[string]map[int64]string
}

func newCSVStore(files map[string]string) *CSVStore {
	s := &CSVStore{
		rowsByBase: map[string][][]string{},
		nameByBase: map[string]map[int64]string{},
	}
	for file, content := range files {
		base := csvBaseName(file)
		if base == "" {
			continue
		}
		rows := parseCSVContent(content)
		s.rowsByBase[base] = rows
		nameMap := map[int64]string{}
		for _, row := range rows {
			if len(row) < 2 {
				continue
			}
			id, err := strconv.ParseInt(strings.TrimSpace(row[0]), 10, 64)
			if err != nil {
				continue
			}
			nameMap[id] = strings.TrimSpace(row[1])
		}
		s.nameByBase[base] = nameMap
	}
	return s
}

func parseCSVContent(raw string) [][]string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")
	rows := make([][]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if i := strings.Index(line, ";"); i >= 0 {
			line = strings.TrimSpace(line[:i])
			if line == "" {
				continue
			}
		}
		r := csv.NewReader(strings.NewReader(line))
		r.FieldsPerRecord = -1
		rec, err := r.Read()
		if err != nil {
			parts := strings.Split(line, ",")
			row := make([]string, 0, len(parts))
			for _, p := range parts {
				row = append(row, strings.TrimSpace(p))
			}
			rows = append(rows, row)
			continue
		}
		for i := range rec {
			rec[i] = strings.TrimSpace(rec[i])
		}
		rows = append(rows, rec)
	}
	return rows
}

func csvBaseName(file string) string {
	up := strings.ToUpper(strings.TrimSpace(file))
	if !strings.HasSuffix(up, ".CSV") {
		return ""
	}
	up = strings.TrimSuffix(up, ".CSV")
	if i := strings.LastIndex(up, "/"); i >= 0 {
		up = up[i+1:]
	}
	if i := strings.LastIndex(up, "\\"); i >= 0 {
		up = up[i+1:]
	}
	return up
}

func (s *CSVStore) Name(base string, id int64) (string, bool) {
	base = strings.ToUpper(strings.TrimSpace(base))
	m := s.nameByBase[base]
	if m == nil {
		return "", false
	}
	v, ok := m[id]
	return v, ok
}

func (s *CSVStore) Exists(base string) bool {
	base = strings.ToUpper(strings.TrimSpace(base))
	_, ok := s.rowsByBase[base]
	return ok
}
