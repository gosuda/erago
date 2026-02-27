package eruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gosuda/erago/ast"
)

type varDataSnapshot struct {
	Format    string                       `json:"format"`
	SavedAt   string                       `json:"saved_at"`
	SaveMes   string                       `json:"save_mes,omitempty"`
	Globals   map[string]saveValue         `json:"globals,omitempty"`
	Arrays    map[string]saveArraySnapshot `json:"arrays,omitempty"`
	VarOrder  []string                     `json:"var_order,omitempty"`
	ArrayList []string                     `json:"array_list,omitempty"`
}

type charaSaveItem struct {
	ID   int64                `json:"id"`
	Vars map[string]saveValue `json:"vars,omitempty"`
}

type charaDataSnapshot struct {
	Format  string          `json:"format"`
	SavedAt string          `json:"saved_at"`
	SaveMes string          `json:"save_mes,omitempty"`
	Indices []int64         `json:"indices"`
	Chars   []charaSaveItem `json:"characters"`
}

func invalidDatName(name string) bool {
	if strings.TrimSpace(name) == "" {
		return true
	}
	if strings.ContainsAny(name, "/\\") {
		return true
	}
	if strings.ContainsRune(name, 0) {
		return true
	}
	if strings.ContainsAny(name, "<>:\"|?*") {
		return true
	}
	return false
}

func (vm *VM) evalDatFilename(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("missing filename")
	}
	v, err := vm.evalLooseExpr(raw)
	name := ""
	if err == nil {
		name = strings.TrimSpace(v.String())
	} else {
		name = strings.TrimSpace(raw)
	}
	if invalidDatName(name) {
		return "", fmt.Errorf("invalid filename %q", name)
	}
	return name, nil
}

func (vm *VM) varDatPath(name string) (string, error) {
	if err := vm.ensureSaveDir(); err != nil {
		return "", err
	}
	return filepath.Join(vm.saveDir, "var_"+name+".dat"), nil
}

func (vm *VM) varDatJSONPath(name string) (string, error) {
	if err := vm.ensureSaveDir(); err != nil {
		return "", err
	}
	return filepath.Join(vm.saveDir, "var_"+name+".json"), nil
}

func (vm *VM) charaDatPath(name string) (string, error) {
	if err := vm.ensureSaveDir(); err != nil {
		return "", err
	}
	return filepath.Join(vm.saveDir, "chara_"+name+".dat"), nil
}

func (vm *VM) charaDatJSONPath(name string) (string, error) {
	if err := vm.ensureSaveDir(); err != nil {
		return "", err
	}
	return filepath.Join(vm.saveDir, "chara_"+name+".json"), nil
}

func valueToSaveValue(v Value) saveValue {
	if v.Kind() == StringKind {
		return saveValue{Kind: "string", S: v.String()}
	}
	return saveValue{Kind: "int", I: v.Int64()}
}

func saveValueToValue(v saveValue) Value {
	if strings.EqualFold(v.Kind, "string") {
		return Str(v.S)
	}
	return Int(v.I)
}

func cloneArrayVar(arr *ArrayVar) *ArrayVar {
	if arr == nil {
		return nil
	}
	cp := newArrayVar(arr.IsString, arr.IsDynamic, append([]int(nil), arr.Dims...))
	for k, v := range arr.Data {
		cp.Data[k] = v
	}
	return cp
}

func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (vm *VM) collectVarSelection(selectors []string) (map[string]Value, map[string]*ArrayVar) {
	globals := map[string]Value{}
	arrays := map[string]*ArrayVar{}
	if len(selectors) == 0 {
		for k, v := range vm.globals {
			globals[k] = v
		}
		for k, arr := range vm.gArrays {
			arrays[k] = cloneArrayVar(arr)
		}
		return globals, arrays
	}

	for _, raw := range selectors {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		ref, err := vm.parseVarRefRuntime(raw)
		if err != nil {
			if v, ev := vm.evalLooseExpr(raw); ev == nil {
				name := strings.ToUpper(strings.TrimSpace(v.String()))
				if name == "" {
					continue
				}
				if arr, ok := vm.lookupArray(name); ok {
					arrays[name] = cloneArrayVar(arr)
				} else {
					globals[name] = vm.getVar(name)
				}
			}
			continue
		}

		base := strings.ToUpper(strings.TrimSpace(ref.Name))
		if base == "" {
			continue
		}
		if len(ref.Index) == 0 {
			if arr, ok := vm.lookupArray(base); ok {
				arrays[base] = cloneArrayVar(arr)
			} else {
				globals[base] = vm.getVar(base)
			}
			continue
		}

		idx, err := vm.evalIndexExprs(ref.Index)
		if err != nil {
			continue
		}
		v, err := vm.getVarRef(ast.VarRef{Name: base, Index: ref.Index})
		if err != nil {
			continue
		}
		src, ok := vm.lookupArray(base)
		if !ok {
			continue
		}
		arr, exists := arrays[base]
		if !exists {
			arr = newArrayVar(src.IsString, src.IsDynamic, append([]int(nil), src.Dims...))
			arr.Data = map[string]Value{}
			arrays[base] = arr
		}
		_ = arr.Set(idx, v)
	}

	return globals, arrays
}

func (vm *VM) buildVarSnapshot(saveMes string, globals map[string]Value, arrays map[string]*ArrayVar) varDataSnapshot {
	snap := varDataSnapshot{
		Format:    "erago.var.v1",
		SavedAt:   time.Now().Format(time.RFC3339Nano),
		SaveMes:   saveMes,
		Globals:   map[string]saveValue{},
		Arrays:    map[string]saveArraySnapshot{},
		VarOrder:  nil,
		ArrayList: nil,
	}
	for _, k := range sortedStringKeys(globals) {
		snap.Globals[k] = valueToSaveValue(globals[k])
		snap.VarOrder = append(snap.VarOrder, k)
	}
	for _, name := range sortedStringKeys(arrays) {
		arr := arrays[name]
		if arr == nil {
			continue
		}
		data := map[string]saveValue{}
		for key, v := range arr.Data {
			data[key] = valueToSaveValue(v)
		}
		snap.Arrays[name] = saveArraySnapshot{
			IsString:  arr.IsString,
			IsDynamic: arr.IsDynamic,
			Dims:      append([]int(nil), arr.Dims...),
			Data:      data,
		}
		snap.ArrayList = append(snap.ArrayList, name)
	}
	return snap
}

func writeVarSnapshotJSON(path string, snap varDataSnapshot) error {
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func readVarSnapshotJSON(data []byte) (varDataSnapshot, error) {
	var snap varDataSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return varDataSnapshot{}, err
	}
	if snap.Globals == nil {
		snap.Globals = map[string]saveValue{}
	}
	if snap.Arrays == nil {
		snap.Arrays = map[string]saveArraySnapshot{}
	}
	return snap, nil
}

func applyVarSnapshot(vm *VM, snap varDataSnapshot) {
	for k, sv := range snap.Globals {
		vm.setVar(strings.ToUpper(k), saveValueToValue(sv))
	}
	for name, saved := range snap.Arrays {
		arr := newArrayVar(saved.IsString, saved.IsDynamic, saved.Dims)
		for key, sv := range saved.Data {
			arr.Data[key] = saveValueToValue(sv)
		}
		vm.gArrays[strings.ToUpper(name)] = arr
	}
}

func parseSaveVarArgs(arg string, vm *VM) (name string, saveMes string, selectors []string, err error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) == 1 {
		parts = strings.Fields(arg)
	}
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", "", nil, fmt.Errorf("SAVEVAR requires filename")
	}
	name, err = vm.evalDatFilename(parts[0])
	if err != nil {
		return "", "", nil, err
	}
	saveMes = ""
	start := 1
	if len(parts) > 1 {
		if v, ev := vm.evalLooseExpr(parts[1]); ev == nil {
			saveMes = v.String()
			start = 2
		}
	}
	if start < len(parts) {
		selectors = append(selectors, parts[start:]...)
	}
	return name, saveMes, selectors, nil
}

func (vm *VM) execSaveVar(arg string) (execResult, error) {
	name, saveMes, selectors, err := parseSaveVarArgs(arg, vm)
	if err != nil {
		return execResult{}, err
	}
	globals, arrays := vm.collectVarSelection(selectors)
	datPath, err := vm.varDatPath(name)
	if err != nil {
		return execResult{}, err
	}
	jsonPath, err := vm.varDatJSONPath(name)
	if err != nil {
		return execResult{}, err
	}

	switch vm.datSaveFormat {
	case "binary":
		if err := vm.writeVarBinaryFile(datPath, saveMes, globals, arrays); err != nil {
			return execResult{}, err
		}
	case "both":
		if err := vm.writeVarBinaryFile(datPath, saveMes, globals, arrays); err != nil {
			return execResult{}, err
		}
		snap := vm.buildVarSnapshot(saveMes, globals, arrays)
		if err := writeVarSnapshotJSON(jsonPath, snap); err != nil {
			return execResult{}, err
		}
	default:
		snap := vm.buildVarSnapshot(saveMes, globals, arrays)
		if err := writeVarSnapshotJSON(datPath, snap); err != nil {
			return execResult{}, err
		}
	}

	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execLoadVar(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) == 1 {
		parts = strings.Fields(arg)
	}
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return execResult{}, fmt.Errorf("LOADVAR requires filename")
	}
	name, err := vm.evalDatFilename(parts[0])
	if err != nil {
		return execResult{}, err
	}
	datPath, err := vm.varDatPath(name)
	if err != nil {
		return execResult{}, err
	}
	jsonPath, err := vm.varDatJSONPath(name)
	if err != nil {
		return execResult{}, err
	}

	loadFromData := func(data []byte) error {
		if unique, version, _, globals, arrays, err := vm.readVarBinaryData(data); err == nil {
			if unique != vm.saveUniqueCode {
				return fmt.Errorf("SAVEVAR incompatible unique code")
			}
			if version != vm.saveVersion {
				return fmt.Errorf("SAVEVAR incompatible version")
			}
			for k, v := range globals {
				vm.setVar(strings.ToUpper(k), v)
			}
			for name, arr := range arrays {
				vm.gArrays[strings.ToUpper(name)] = arr
			}
			return nil
		}
		snap, err := readVarSnapshotJSON(data)
		if err != nil {
			return err
		}
		applyVarSnapshot(vm, snap)
		return nil
	}

	if b, err := os.ReadFile(datPath); err == nil {
		if err := loadFromData(b); err != nil {
			return execResult{}, err
		}
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	} else if !os.IsNotExist(err) {
		return execResult{}, err
	}

	if b, err := os.ReadFile(jsonPath); err == nil {
		snap, err := readVarSnapshotJSON(b)
		if err != nil {
			return execResult{}, err
		}
		applyVarSnapshot(vm, snap)
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	} else if !os.IsNotExist(err) {
		return execResult{}, err
	}

	vm.globals["RESULT"] = Int(0)
	return execResult{kind: resultNone}, nil
}

func parseSaveCharaArgs(arg string, vm *VM) (name, saveMes string, indices []int64, err error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 2 {
		parts = strings.Fields(arg)
	}
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", "", nil, fmt.Errorf("SAVECHARA requires filename")
	}
	name, err = vm.evalDatFilename(parts[0])
	if err != nil {
		return "", "", nil, err
	}
	saveMes = ""
	startIdx := 1
	if len(parts) > 1 {
		if v, ev := vm.evalLooseExpr(parts[1]); ev == nil {
			saveMes = v.String()
			startIdx = 2
		}
	}
	if startIdx >= len(parts) {
		for i := range vm.characters {
			indices = append(indices, int64(i))
		}
		return
	}
	seen := map[int64]bool{}
	for _, raw := range parts[startIdx:] {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		v, err := vm.evalLooseExpr(raw)
		if err != nil {
			return "", "", nil, err
		}
		idx := v.Int64()
		if idx < 0 || idx >= int64(len(vm.characters)) {
			return "", "", nil, fmt.Errorf("SAVECHARA index out of range: %d", idx)
		}
		if seen[idx] {
			return "", "", nil, fmt.Errorf("SAVECHARA duplicate index: %d", idx)
		}
		seen[idx] = true
		indices = append(indices, idx)
	}
	return
}

func buildCharaSnapshot(saveMes string, indices []int64, chars []RuntimeCharacter) charaDataSnapshot {
	snap := charaDataSnapshot{
		Format:  "erago.chara.v1",
		SavedAt: time.Now().Format(time.RFC3339Nano),
		SaveMes: saveMes,
		Indices: append([]int64(nil), indices...),
		Chars:   make([]charaSaveItem, 0, len(chars)),
	}
	for _, ch := range chars {
		vars := map[string]saveValue{}
		for k, v := range ch.Vars {
			vars[k] = valueToSaveValue(v)
		}
		snap.Chars = append(snap.Chars, charaSaveItem{ID: ch.ID, Vars: vars})
	}
	return snap
}

func writeCharaSnapshotJSON(path string, snap charaDataSnapshot) error {
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func readCharaSnapshotJSON(data []byte) (charaDataSnapshot, error) {
	var snap charaDataSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return charaDataSnapshot{}, err
	}
	return snap, nil
}

func appendLoadedCharacters(vm *VM, chars []RuntimeCharacter) {
	for _, item := range chars {
		vm.characters = append(vm.characters, RuntimeCharacter{ID: item.ID, Vars: item.Vars})
		if item.ID >= vm.nextCharID {
			vm.nextCharID = item.ID + 1
		}
	}
	vm.refreshCharacterGlobals()
}

func (vm *VM) execSaveChara(arg string) (execResult, error) {
	name, saveMes, indices, err := parseSaveCharaArgs(arg, vm)
	if err != nil {
		return execResult{}, err
	}
	selected := make([]RuntimeCharacter, 0, len(indices))
	for _, idx := range indices {
		if idx < 0 || idx >= int64(len(vm.characters)) {
			continue
		}
		ch := vm.characters[idx]
		vars := map[string]Value{}
		for k, v := range ch.Vars {
			vars[k] = v
		}
		selected = append(selected, RuntimeCharacter{ID: ch.ID, Vars: vars})
	}

	datPath, err := vm.charaDatPath(name)
	if err != nil {
		return execResult{}, err
	}
	jsonPath, err := vm.charaDatJSONPath(name)
	if err != nil {
		return execResult{}, err
	}

	switch vm.datSaveFormat {
	case "binary":
		if err := vm.writeCharaBinaryFile(datPath, saveMes, selected); err != nil {
			return execResult{}, err
		}
	case "both":
		if err := vm.writeCharaBinaryFile(datPath, saveMes, selected); err != nil {
			return execResult{}, err
		}
		snap := buildCharaSnapshot(saveMes, indices, selected)
		if err := writeCharaSnapshotJSON(jsonPath, snap); err != nil {
			return execResult{}, err
		}
	default:
		snap := buildCharaSnapshot(saveMes, indices, selected)
		if err := writeCharaSnapshotJSON(datPath, snap); err != nil {
			return execResult{}, err
		}
	}

	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execLoadChara(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) == 1 {
		parts = strings.Fields(arg)
	}
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return execResult{}, fmt.Errorf("LOADCHARA requires filename")
	}
	name, err := vm.evalDatFilename(parts[0])
	if err != nil {
		return execResult{}, err
	}
	datPath, err := vm.charaDatPath(name)
	if err != nil {
		return execResult{}, err
	}
	jsonPath, err := vm.charaDatJSONPath(name)
	if err != nil {
		return execResult{}, err
	}

	loadFromData := func(data []byte) error {
		if unique, version, _, chars, err := vm.readCharaBinaryData(data); err == nil {
			if unique != vm.saveUniqueCode {
				return fmt.Errorf("SAVECHARA incompatible unique code")
			}
			if version != vm.saveVersion {
				return fmt.Errorf("SAVECHARA incompatible version")
			}
			appendLoadedCharacters(vm, chars)
			return nil
		}
		snap, err := readCharaSnapshotJSON(data)
		if err != nil {
			return err
		}
		chars := make([]RuntimeCharacter, 0, len(snap.Chars))
		for _, item := range snap.Chars {
			vars := map[string]Value{}
			for k, sv := range item.Vars {
				vars[k] = saveValueToValue(sv)
			}
			chars = append(chars, RuntimeCharacter{ID: item.ID, Vars: vars})
		}
		appendLoadedCharacters(vm, chars)
		return nil
	}

	if b, err := os.ReadFile(datPath); err == nil {
		if err := loadFromData(b); err != nil {
			return execResult{}, err
		}
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	} else if !os.IsNotExist(err) {
		return execResult{}, err
	}

	if b, err := os.ReadFile(jsonPath); err == nil {
		snap, err := readCharaSnapshotJSON(b)
		if err != nil {
			return execResult{}, err
		}
		chars := make([]RuntimeCharacter, 0, len(snap.Chars))
		for _, item := range snap.Chars {
			vars := map[string]Value{}
			for k, sv := range item.Vars {
				vars[k] = saveValueToValue(sv)
			}
			chars = append(chars, RuntimeCharacter{ID: item.ID, Vars: vars})
		}
		appendLoadedCharacters(vm, chars)
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	} else if !os.IsNotExist(err) {
		return execResult{}, err
	}

	vm.globals["RESULT"] = Int(0)
	return execResult{kind: resultNone}, nil
}
