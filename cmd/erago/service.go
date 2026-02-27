package main

import (
	"fmt"
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

	err = runWithEntryFallback(vm, cfg.entry)
	events <- vmDoneMsg{err: err}
}

func runWithEntryFallback(vm *eruntime.VM, preferred string) error {
	candidates := []string{
		strings.TrimSpace(preferred),
		"TITLE",
		"SYSTEM_TITLE",
		"START",
		"SYSTEM_START",
		"START_SELECT",
	}
	seen := map[string]struct{}{}
	var lastErr error
	for _, c := range candidates {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		_, err := vm.Run(c)
		if err == nil {
			return nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "function "+c+" not found") {
			return err
		}
	}
	return lastErr
}
