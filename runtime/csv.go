package eruntime

import (
	"encoding/csv"
	"strconv"
	"strings"
)

type CSVStore struct {
	rowsByBase     map[string][][]string
	nameByBase     map[string]map[int64]string
	charaExists    map[int64]struct{}
	gameCode       int64
	gameVersion    int64
	hasGameCode    bool
	hasGameVersion bool
	gameTitle      string
	gameAuthor     string
	gameYear       string
	windowTitle    string
	gameInfo       string
}

func newCSVStore(files map[string]string) *CSVStore {
	s := &CSVStore{
		rowsByBase:  map[string][][]string{},
		nameByBase:  map[string]map[int64]string{},
		charaExists: map[int64]struct{}{},
		gameCode:    0,
		gameVersion: 0,
	}
	for file, content := range files {
		base := csvBaseName(file)
		if base == "" {
			continue
		}
		if id, ok := charaIDFromBase(base); ok {
			s.charaExists[id] = struct{}{}
		}
		rows := parseCSVContent(content)
		s.rowsByBase[base] = rows
		if base == "GAMEBASE" {
			for _, row := range rows {
				if len(row) < 2 {
					continue
				}
				key := strings.TrimSpace(row[0])
				val := strings.TrimSpace(row[1])
				switch strings.ToUpper(key) {
				case "CODE", "コード", "코드":
					if n, err := strconv.ParseInt(val, 10, 64); err == nil {
						s.gameCode = n
						s.hasGameCode = true
					}
				case "VERSION", "バージョン", "버전":
					if n, err := strconv.ParseInt(val, 10, 64); err == nil {
						s.gameVersion = n
						s.hasGameVersion = true
					}
				case "TITLE", "タイトル", "타이틀":
					s.gameTitle = val
				case "AUTHOR", "作者", "작자", "저자":
					s.gameAuthor = val
				case "YEAR", "製作年", "제작년":
					s.gameYear = val
				case "WINDOWTITLE", "ウィンドウタイトル", "윈도우타이틀":
					s.windowTitle = val
				case "INFO", "追加情報", "추가정보":
					s.gameInfo = val
				}
			}
		}
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
	raw = strings.TrimPrefix(raw, "\uFEFF")
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

func (s *CSVStore) ExistsID(id int64) bool {
	if _, ok := s.charaExists[id]; ok {
		return true
	}
	if m := s.nameByBase["RELATION"]; m != nil {
		_, ok := m[id]
		return ok
	}
	return false
}

func (s *CSVStore) FindID(base, name string) (int64, bool) {
	base = strings.ToUpper(strings.TrimSpace(base))
	rows := s.rowsByBase[base]
	if len(rows) == 0 {
		return 0, false
	}
	target := strings.TrimSpace(name)
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(row[1]), target) {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSpace(row[0]), 10, 64)
		if err != nil {
			continue
		}
		return id, true
	}
	return 0, false
}

func (s *CSVStore) GameCodeVersion() (code int64, version int64, hasCode bool, hasVersion bool) {
	return s.gameCode, s.gameVersion, s.hasGameCode, s.hasGameVersion
}

func (s *CSVStore) GameMeta() (title, author, year, windowTitle, info string) {
	return s.gameTitle, s.gameAuthor, s.gameYear, s.windowTitle, s.gameInfo
}

func charaIDFromBase(base string) (int64, bool) {
	base = strings.ToUpper(strings.TrimSpace(base))
	if !strings.HasPrefix(base, "CHARA") {
		return 0, false
	}
	i := len("CHARA")
	for i < len(base) && base[i] >= '0' && base[i] <= '9' {
		i++
	}
	if i == len("CHARA") {
		return 0, false
	}
	id, err := strconv.ParseInt(base[len("CHARA"):i], 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
