package mobile

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gosuda/erago"
	eruntime "github.com/gosuda/erago/runtime"
)

type runResult struct {
	Outputs []eruntime.Output `json:"outputs"`
	Error   string            `json:"error,omitempty"`
}

// Run executes ERB/ERH/CSV content provided as JSON map and returns JSON result.
// filesJSON format: {"ERB/MAIN.ERB":"...","ERH/MAIN.ERH":"...","CSV/GAMEBASE.CSV":"..."}
// inputsJSON format: ["1","hello", ...]
func Run(filesJSON, entry, inputsJSON, saveFmt string) string {
	result := runResult{Outputs: nil}

	var files map[string]string
	if err := json.Unmarshal([]byte(filesJSON), &files); err != nil {
		result.Error = fmt.Sprintf("invalid files json: %v", err)
		b, _ := json.Marshal(result)
		return string(b)
	}
	if len(files) == 0 {
		result.Error = "no files provided"
		b, _ := json.Marshal(result)
		return string(b)
	}

	entry = strings.TrimSpace(entry)
	if entry == "" {
		entry = "TITLE"
	}

	saveFmt = strings.ToLower(strings.TrimSpace(saveFmt))
	if saveFmt == "" {
		saveFmt = "json"
	}

	var queued []string
	if strings.TrimSpace(inputsJSON) != "" {
		if err := json.Unmarshal([]byte(inputsJSON), &queued); err != nil {
			result.Error = fmt.Sprintf("invalid inputs json: %v", err)
			b, _ := json.Marshal(result)
			return string(b)
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

	out, err := vm.Run(entry)
	if err != nil {
		result.Error = fmt.Sprintf("runtime: %v", err)
	} else {
		result.Outputs = out
	}

	b, _ := json.Marshal(result)
	return string(b)
}
