package eruntime

import (
	"sort"
	"strings"
)

type UIState struct {
	Align      string
	Color      string
	BgColor    string
	FocusColor string
	Font       string
	Bold       bool
	Italic     bool
	SkipDisp   bool
	Redraw     bool
	PrintCPL   int64
}

type RuntimeCharacter struct {
	ID   int64
	Vars map[string]Value
}

func defaultUIState() UIState {
	return UIState{
		Align:      "LEFT",
		Color:      "FFFFFF",
		BgColor:    "000000",
		FocusColor: "FFFF00",
		Font:       "",
		Bold:       false,
		Italic:     false,
		SkipDisp:   false,
		Redraw:     true,
		PrintCPL:   3,
	}
}

func (vm *VM) refreshCharacterGlobals() {
	vm.globals["CHARANUM"] = Int(int64(len(vm.characters)))
}

func (vm *VM) addCharacter(id int64) int64 {
	if id < 0 {
		id = vm.nextCharID
		vm.nextCharID++
	}
	vm.characters = append(vm.characters, RuntimeCharacter{ID: id, Vars: map[string]Value{}})
	vm.refreshCharacterGlobals()
	return int64(len(vm.characters) - 1)
}

func (vm *VM) deleteCharacterAt(idx int64) bool {
	if idx < 0 || idx >= int64(len(vm.characters)) {
		return false
	}
	i := int(idx)
	vm.characters = append(vm.characters[:i], vm.characters[i+1:]...)
	vm.refreshCharacterGlobals()
	return true
}

func (vm *VM) sortCharacters() {
	sort.Slice(vm.characters, func(i, j int) bool {
		return vm.characters[i].ID < vm.characters[j].ID
	})
}

func normalizeAlign(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	switch s {
	case "LEFT", "CENTER", "RIGHT":
		return s
	default:
		return "LEFT"
	}
}
