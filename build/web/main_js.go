//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"syscall/js"

	"github.com/gosuda/erago"
	eruntime "github.com/gosuda/erago/runtime"
)

type runResult struct {
	Outputs []eruntime.Output `json:"outputs"`
	Error   string            `json:"error,omitempty"`
}

type inputRequestPayload struct {
	Command    string `json:"command"`
	Timed      bool   `json:"timed"`
	TimeoutMs  int64  `json:"timeout_ms"`
	HasDefault bool   `json:"has_default"`
	Default    string `json:"default"`
}

const timeoutSentinel = "__ERAGO_TIMEOUT__"
const abortSentinel = "__ERAGO_ABORT__"

func inputPrompt(req eruntime.InputRequest) (string, bool, error) {
	fn := js.Global().Get("eragoInputNext")
	if fn.Type() != js.TypeFunction {
		if req.Timed {
			return "", true, nil
		}
		if req.HasDefault {
			return req.DefaultValue.String(), false, nil
		}
		return "", false, nil
	}

	payload := inputRequestPayload{
		Command:    strings.ToUpper(strings.TrimSpace(req.Command)),
		Timed:      req.Timed,
		TimeoutMs:  req.TimeoutMs,
		HasDefault: req.HasDefault,
		Default:    req.DefaultValue.String(),
	}
	b, _ := json.Marshal(payload)
	v := fn.Invoke(string(b))
	if v.IsUndefined() || v.IsNull() {
		if req.Timed {
			return "", true, nil
		}
		if req.HasDefault {
			return req.DefaultValue.String(), false, nil
		}
		return "", false, nil
	}
	out := strings.TrimSpace(v.String())
	if out == timeoutSentinel {
		return "", true, nil
	}
	if out == abortSentinel {
		return "", false, fmt.Errorf("input queue is empty for %s (add input and run again)", payload.Command)
	}
	return out, false, nil
}

func runGame(this js.Value, args []js.Value) any {
	result := runResult{Outputs: nil}
	if len(args) < 1 {
		result.Error = "runGame requires files JSON object"
		b, _ := json.Marshal(result)
		return string(b)
	}

	var files map[string]string
	if err := json.Unmarshal([]byte(args[0].String()), &files); err != nil {
		result.Error = fmt.Sprintf("invalid files json: %v", err)
		b, _ := json.Marshal(result)
		return string(b)
	}
	if len(files) == 0 {
		result.Error = "no files provided"
		b, _ := json.Marshal(result)
		return string(b)
	}

	entry := "TITLE"
	if len(args) > 1 {
		entry = strings.TrimSpace(args[1].String())
		if entry == "" {
			entry = "TITLE"
		}
	}

	saveFmt := "json"
	if len(args) > 2 {
		sf := strings.ToLower(strings.TrimSpace(args[2].String()))
		if sf != "" {
			saveFmt = sf
		}
	}

	var queued []string
	if len(args) > 3 {
		if strings.TrimSpace(args[3].String()) != "" {
			_ = json.Unmarshal([]byte(args[3].String()), &queued)
		}
	}

	vm, err := erago.Compile(files)
	if err != nil {
		result.Error = fmt.Sprintf("compile: %v", err)
		b, _ := json.Marshal(result)
		return string(b)
	}
	if err := vm.SetDatSaveFormat(saveFmt); err != nil {
		result.Error = fmt.Sprintf("save format: %v", err)
		b, _ := json.Marshal(result)
		return string(b)
	}
	if len(queued) > 0 {
		vm.EnqueueInput(queued...)
	}
	vm.SetInputProvider(inputPrompt)

	out, err := vm.Run(entry)
	if err != nil {
		result.Error = fmt.Sprintf("runtime: %v", err)
	} else {
		result.Outputs = out
	}

	b, _ := json.Marshal(result)
	return string(b)
}

func main() {
	js.Global().Set("eragoRun", js.FuncOf(runGame))
	select {}
}
