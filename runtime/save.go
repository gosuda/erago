package eruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type saveValue struct {
	Kind string `json:"kind"`
	I    int64  `json:"i,omitempty"`
	S    string `json:"s,omitempty"`
}

type saveSnapshot struct {
	Globals map[string]saveValue         `json:"globals"`
	GArrays map[string]saveArraySnapshot `json:"g_arrays,omitempty"`
}

type saveArraySnapshot struct {
	IsString  bool                 `json:"is_string"`
	IsDynamic bool                 `json:"is_dynamic,omitempty"`
	Dims      []int                `json:"dims"`
	Data      map[string]saveValue `json:"data"`
}

func (vm *VM) ensureSaveDir() error {
	if vm.saveDir == "" {
		vm.saveDir = filepath.Join(".", ".erago_saves")
	}
	return os.MkdirAll(vm.saveDir, 0o755)
}

func (vm *VM) savePath(slot string) (string, error) {
	if err := vm.ensureSaveDir(); err != nil {
		return "", err
	}
	if slot == "" {
		slot = "default"
	}
	return filepath.Join(vm.saveDir, slot+".json"), nil
}

func (vm *VM) saveGlobals(slot string) error {
	path, err := vm.savePath(slot)
	if err != nil {
		return err
	}
	snap := saveSnapshot{
		Globals: map[string]saveValue{},
		GArrays: map[string]saveArraySnapshot{},
	}
	for k, v := range vm.globals {
		sv := saveValue{}
		if v.Kind() == StringKind {
			sv.Kind = "string"
			sv.S = v.String()
		} else {
			sv.Kind = "int"
			sv.I = v.Int64()
		}
		snap.Globals[k] = sv
	}
	for name, arr := range vm.gArrays {
		cpDims := make([]int, len(arr.Dims))
		copy(cpDims, arr.Dims)
		data := map[string]saveValue{}
		for key, v := range arr.Data {
			sv := saveValue{}
			if v.Kind() == StringKind {
				sv.Kind = "string"
				sv.S = v.String()
			} else {
				sv.Kind = "int"
				sv.I = v.Int64()
			}
			data[key] = sv
		}
		snap.GArrays[name] = saveArraySnapshot{
			IsString:  arr.IsString,
			IsDynamic: arr.IsDynamic,
			Dims:      cpDims,
			Data:      data,
		}
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal save: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write save: %w", err)
	}
	return nil
}

func (vm *VM) loadGlobals(slot string) (bool, error) {
	path, err := vm.savePath(slot)
	if err != nil {
		return false, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var snap saveSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return false, fmt.Errorf("parse save: %w", err)
	}
	for k, sv := range snap.Globals {
		if sv.Kind == "string" {
			vm.globals[k] = Str(sv.S)
		} else {
			vm.globals[k] = Int(sv.I)
		}
	}
	for name, saved := range snap.GArrays {
		arr := newArrayVar(saved.IsString, saved.IsDynamic, saved.Dims)
		for key, sv := range saved.Data {
			if sv.Kind == "string" {
				arr.Data[key] = Str(sv.S)
			} else {
				arr.Data[key] = Int(sv.I)
			}
		}
		vm.gArrays[name] = arr
	}
	return true, nil
}

func (vm *VM) deleteSave(slot string) error {
	path, err := vm.savePath(slot)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (vm *VM) hasSave(slot string) (bool, error) {
	path, err := vm.savePath(slot)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
