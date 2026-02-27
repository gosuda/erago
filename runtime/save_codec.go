package eruntime

import (
	"fmt"
	"os"
	"strings"
)

func ConvertDatFile(kind, inputPath, outputPath, outputFormat string) error {
	kind = strings.ToLower(strings.TrimSpace(kind))
	outputFormat = strings.ToLower(strings.TrimSpace(outputFormat))
	if kind != "var" && kind != "chara" {
		return fmt.Errorf("unsupported kind %q (use var|chara)", kind)
	}
	if outputFormat != "json" && outputFormat != "binary" {
		return fmt.Errorf("unsupported output format %q (use json|binary)", outputFormat)
	}

	data, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}
	isBin := IsEraBinaryData(data)

	saveMes := ""
	unique := int64(0)
	version := int64(1)

	switch kind {
	case "var":
		globals := map[string]Value{}
		arrays := map[string]*ArrayVar{}
		if isBin {
			vm := &VM{saveUniqueCode: 0, saveVersion: 1}
			u, v, mes, g, a, err := vm.readVarBinaryData(data)
			if err != nil {
				return err
			}
			unique, version, saveMes = u, v, mes
			globals, arrays = g, a
		} else {
			snap, err := readVarSnapshotJSON(data)
			if err != nil {
				return err
			}
			saveMes = snap.SaveMes
			for k, sv := range snap.Globals {
				globals[k] = saveValueToValue(sv)
			}
			for name, saved := range snap.Arrays {
				arr := newArrayVar(saved.IsString, saved.IsDynamic, saved.Dims)
				for key, sv := range saved.Data {
					arr.Data[key] = saveValueToValue(sv)
				}
				arrays[name] = arr
			}
		}
		if outputFormat == "json" {
			vm := &VM{}
			snap := vm.buildVarSnapshot(saveMes, globals, arrays)
			return writeVarSnapshotJSON(outputPath, snap)
		}
		vm := &VM{saveUniqueCode: unique, saveVersion: version}
		return vm.writeVarBinaryFile(outputPath, saveMes, globals, arrays)

	case "chara":
		chars := []RuntimeCharacter{}
		indices := []int64{}
		if isBin {
			vm := &VM{saveUniqueCode: 0, saveVersion: 1}
			u, v, mes, list, err := vm.readCharaBinaryData(data)
			if err != nil {
				return err
			}
			unique, version, saveMes = u, v, mes
			chars = list
			for i := range chars {
				indices = append(indices, int64(i))
			}
		} else {
			snap, err := readCharaSnapshotJSON(data)
			if err != nil {
				return err
			}
			saveMes = snap.SaveMes
			indices = append(indices, snap.Indices...)
			for _, item := range snap.Chars {
				vars := map[string]Value{}
				for k, sv := range item.Vars {
					vars[k] = saveValueToValue(sv)
				}
				chars = append(chars, RuntimeCharacter{ID: item.ID, Vars: vars})
			}
		}
		if outputFormat == "json" {
			snap := buildCharaSnapshot(saveMes, indices, chars)
			return writeCharaSnapshotJSON(outputPath, snap)
		}
		vm := &VM{saveUniqueCode: unique, saveVersion: version}
		return vm.writeCharaBinaryFile(outputPath, saveMes, chars)
	}

	return nil
}
