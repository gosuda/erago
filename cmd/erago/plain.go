package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gosuda/erago"
	eruntime "github.com/gosuda/erago/runtime"
)

func runPlain(cfg appConfig) error {
	files, err := loadScripts(cfg.base)
	if err != nil {
		return fmt.Errorf("load scripts: %w", err)
	}
	vm, err := erago.Compile(files)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}
	if err := vm.SetDatSaveFormat("binary"); err != nil {
		return fmt.Errorf("save format: %w", err)
	}
	vm.SetSaveDir(cfg.base)

	reader := bufio.NewReader(os.Stdin)

	vm.SetOutputHook(func(out eruntime.Output) {
		if out.ClearLines > 0 {
			return
		}
		if out.NewLine {
			fmt.Println(out.Text)
		} else {
			fmt.Print(out.Text)
		}
	})

	vm.SetInputProvider(func(req eruntime.InputRequest) (string, bool, error) {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", false, err
		}
		line = strings.TrimRight(line, "\r\n")
		if req.OneInput && !req.Numeric {
			r := []rune(line)
			if len(r) > 0 {
				line = string(r[0])
			} else {
				line = ""
			}
		}
		return line, false, nil
	})

	return runWithEntryFallback(vm, cfg.entry)
}
