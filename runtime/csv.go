package eruntime

import (
	"encoding/csv"
	"strconv"
	"strings"
)

type CSVStore struct {
	rowsByBase     map[string][][]string
	charaRowsByID  map[int64][][]string
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
		rowsByBase:    map[string][][]string{},
		charaRowsByID: map[int64][][]string{},
		nameByBase:    map[string]map[int64]string{},
		charaExists:   map[int64]struct{}{},
		gameCode:      0,
		gameVersion:   0,
	}
	for file, content := range files {
		base := csvBaseName(file)
		if base == "" {
			continue
		}
		rows := parseCSVContent(content)
		s.rowsByBase[base] = rows
		if id, ok := charaIDFromBase(base); ok {
			s.charaExists[id] = struct{}{}
			s.charaRowsByID[id] = rows
		}
		if base == "GAMEBASE" {
			for _, row := range rows {
				if len(row) < 2 {
					continue
				}
				key := strings.TrimSpace(row[0])
				val := strings.TrimSpace(row[1])
				switch strings.ToUpper(key) {
				case "CODE", "\uCF54\uB4DC":
					if n, err := strconv.ParseInt(val, 10, 64); err == nil {
						s.gameCode = n
						s.hasGameCode = true
					}
				case "VERSION", "\uBC84\uC804":
					if n, err := strconv.ParseInt(val, 10, 64); err == nil {
						s.gameVersion = n
						s.hasGameVersion = true
					}
				case "TITLE", "\uD0C0\uC774\uD2C0":
					s.gameTitle = val
				case "AUTHOR", "\uC791\uC790":
					s.gameAuthor = val
				case "YEAR", "\uC2DC\uC791\uB144":
					s.gameYear = val
				case "WINDOWTITLE", "\uC708\uB3C4\uC6B0\uD0C0\uC774\uD2C0":
					s.windowTitle = val
				case "INFO", "\uCD94\uAC00\uC815\uBCF4":
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
		s.ingestCharacterNameRows(base, rows)
	}
	return s
}

func (s *CSVStore) ingestCharacterNameRows(base string, rows [][]string) {
	id, ok := charaIDFromBase(base)
	if !ok {
		return
	}
	name := ""
	callName := ""
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		key := strings.TrimSpace(row[0])
		val := strings.TrimSpace(row[1])
		switch key {
		case "\u756A\u53F7", "\uBC88\uD638", "NO", "ID":
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				id = n
			}
		case "\u540D\u524D", "\uC774\uB984", "NAME":
			name = val
		case "\u547C\u3073\u540D", "\uD638\uCE6D", "CALLNAME":
			callName = val
		}
	}
	if name == "" && callName == "" {
		return
	}
	if name == "" {
		name = callName
	}
	if callName == "" {
		callName = name
	}
	nameMap := s.nameByBase["NAME"]
	if nameMap == nil {
		nameMap = map[int64]string{}
		s.nameByBase["NAME"] = nameMap
	}
	callMap := s.nameByBase["CALLNAME"]
	if callMap == nil {
		callMap = map[int64]string{}
		s.nameByBase["CALLNAME"] = callMap
	}
	nameMap[id] = name
	callMap[id] = callName
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

func (s *CSVStore) CharaField(id int64, section, key string) (string, bool) {
	rows := s.charaRowsByID[id]
	if len(rows) == 0 {
		return "", false
	}
	section = strings.ToUpper(strings.TrimSpace(section))
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		if !csvSectionMatches(section, row[0]) {
			continue
		}
		if strings.TrimSpace(row[1]) != key {
			continue
		}
		if len(row) >= 3 {
			return strings.TrimSpace(row[2]), true
		}
		return "", true
	}
	return "", false
}

func csvSectionMatches(section, actual string) bool {
	actual = strings.TrimSpace(actual)
	switch section {
	case "CSTR":
		return strings.EqualFold(actual, "CSTR")
	case "BASE":
		return actual == "\u57FA\u790E" || strings.EqualFold(actual, "BASE")
	case "TALENT":
		return actual == "\u7D20\u8CEA" || strings.EqualFold(actual, "TALENT")
	case "ABL":
		return actual == "\u80FD\u529B" || strings.EqualFold(actual, "ABL")
	case "EXP":
		return actual == "\u7D4C\u9A13" || strings.EqualFold(actual, "EXP")
	case "RELATION":
		return actual == "\u76F8\u6027" || strings.EqualFold(actual, "RELATION")
	case "EQUIP":
		return actual == "\u88C5\u7740\u7269" || strings.EqualFold(actual, "EQUIP")
	default:
		return strings.EqualFold(actual, section)
	}
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

func (s *CSVStore) GetCSVMap(name string) map[string]string {
	name = strings.ToUpper(strings.TrimSpace(name))
	m := s.nameByBase[name]
	if m == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[strconv.FormatInt(k, 10)] = v
	}
	return result
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
