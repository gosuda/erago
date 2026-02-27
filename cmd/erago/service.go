package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/gosuda/erago"
	eruntime "github.com/gosuda/erago/runtime"
)

func runVM(cfg appConfig, events chan<- tea.Msg) {
	defer close(events)
	files, err := loadScripts(cfg.base)
	if err != nil {
		events <- vmDoneMsg{err: fmt.Errorf("load scripts: %w", err)}
		return
	}
	vm, err := erago.Compile(files)
	if err != nil {
		events <- vmDoneMsg{err: fmt.Errorf("compile: %w", err)}
		return
	}
	if err := vm.SetDatSaveFormat("binary"); err != nil {
		events <- vmDoneMsg{err: fmt.Errorf("save format: %w", err)}
		return
	}
	vm.SetSaveDir(cfg.base)

	vm.SetOutputHook(func(out eruntime.Output) {
		events <- vmOutputMsg{out: out}
	})
	vm.SetInputProvider(func(req eruntime.InputRequest) (string, bool, error) {
		resp := make(chan vmInputResp, 1)
		events <- vmPromptMsg{req: req, resp: resp}
		r := <-resp
		return r.value, r.timeout, nil
	})

	_, err = vm.Run(cfg.entry)
	events <- vmDoneMsg{err: err}
}

func loadScripts(root string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToUpper(filepath.Ext(path))
		if ext != ".ERB" && ext != ".ERH" && ext != ".CSV" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = filepath.Base(path)
		}
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no script files found under %s", root)
	}
	return files, nil
}
