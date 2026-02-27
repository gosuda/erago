package eruntime

import (
	"strconv"
	"strings"
)

type InputPhase string

const (
	InputIdle   InputPhase = "idle"
	InputWait   InputPhase = "wait"
	InputPrompt InputPhase = "input"
)

type InputRequest struct {
	Command        string
	Numeric        bool
	OneInput       bool
	Timed          bool
	TimeoutMs      int64
	Countdown      bool
	Nullable       bool
	HasDefault     bool
	DefaultValue   Value
	TimeoutMessage string
}

type InputState struct {
	Phase         InputPhase
	Current       *InputRequest
	Queue         []string
	LastValue     string
	LastTimeout   bool
	MouseX        int
	MouseY        int
	MouseLeft     bool
	MouseRight    bool
	MouseMiddle   bool
	KeysDown      map[string]bool
	KeysTriggered map[string]bool
	ClientWidth   int
	ClientHeight  int
}

func defaultInputState() InputState {
	return InputState{
		Phase:         InputIdle,
		Current:       nil,
		Queue:         nil,
		LastValue:     "",
		LastTimeout:   false,
		KeysDown:      map[string]bool{},
		KeysTriggered: map[string]bool{},
		ClientWidth:   800,
		ClientHeight:  600,
	}
}

func (vm *VM) EnqueueInput(values ...string) {
	vm.input.Queue = append(vm.input.Queue, values...)
}

func (vm *VM) beginInputRequest(req InputRequest) {
	vm.input.Phase = InputPrompt
	if !req.Numeric && !req.Timed && !req.Nullable && !req.OneInput && req.Command == "WAIT" {
		vm.input.Phase = InputWait
	}
	cp := req
	vm.input.Current = &cp
}

func (vm *VM) finishInputRequest(value string, timeout bool) {
	vm.input.LastValue = value
	vm.input.LastTimeout = timeout
	vm.input.Current = nil
	vm.input.Phase = InputIdle
}

func (vm *VM) consumeQueuedInput() (string, bool) {
	if len(vm.input.Queue) == 0 {
		return "", false
	}
	v := vm.input.Queue[0]
	vm.input.Queue = vm.input.Queue[1:]
	return v, true
}

func normalizeOneDigit(v int64) int64 {
	if v < 0 {
		v = -v
	}
	if v < 10 {
		return v
	}
	s := strconv.FormatInt(v, 10)
	if len(s) == 0 {
		return 0
	}
	n, err := strconv.ParseInt(s[:1], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func firstRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[0])
}

func parseIntInput(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	// Emuera users often input full-width numerals via IME.
	// Normalize common full-width signs/digits before parsing.
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= '０' && r <= '９':
			b.WriteRune('0' + (r - '０'))
		case r == '＋':
			b.WriteByte('+')
		case r == '－' || r == 'ー' || r == '―' || r == '−':
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	raw = strings.TrimSpace(b.String())
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func (vm *VM) maybeEchoInput(text string) {
	if vm.ui.SkipDisp {
		return
	}
	vm.emitOutput(Output{Text: text, NewLine: true})
}

func (vm *VM) resolveInput(req InputRequest) (string, bool, error) {
	// New input boundary: reset watchdog so long-running interactive flows
	// are only flagged when they stop making input progress.
	vm.execSteps = 0
	vm.beginInputRequest(req)
	raw, ok := vm.consumeQueuedInput()
	if !ok {
		if vm.inputProvider != nil {
			value, timeout, err := vm.inputProvider(req)
			if err != nil {
				vm.finishInputRequest("", false)
				return "", false, err
			}
			if timeout && req.TimeoutMessage != "" {
				vm.maybeEchoInput(req.TimeoutMessage)
			}
			vm.finishInputRequest(value, timeout)
			return value, timeout, nil
		}
		if req.Timed {
			if req.TimeoutMessage != "" {
				vm.maybeEchoInput(req.TimeoutMessage)
			}
			if req.HasDefault {
				value := req.DefaultValue.String()
				vm.finishInputRequest(value, true)
				return value, true, nil
			}
			vm.finishInputRequest("", true)
			return "", true, nil
		}
		if req.HasDefault {
			value := req.DefaultValue.String()
			vm.finishInputRequest(value, false)
			return value, false, nil
		}
		vm.finishInputRequest("", false)
		return "", false, nil
	}
	vm.finishInputRequest(raw, false)
	return raw, false, nil
}

func (vm *VM) execWaitLike(name, arg string) (execResult, error) {
	req := InputRequest{Command: name, Numeric: false, OneInput: false, Timed: false, Nullable: false, HasDefault: false}
	if name == "AWAIT" || name == "TWAIT" {
		req.Timed = true
		if strings.TrimSpace(arg) != "" {
			parts := splitTopLevelRuntime(arg, ',')
			if len(parts) == 0 {
				parts = strings.Fields(arg)
			}
			if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
				v, err := vm.evalLooseExpr(parts[0])
				if err == nil {
					req.TimeoutMs = v.Int64()
				}
			}
		}
	}
	_, _, err := vm.resolveInput(req)
	if err != nil {
		return execResult{}, err
	}
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execInputIntLike(name, arg string) (execResult, error) {
	req := InputRequest{Command: name, Numeric: true, OneInput: strings.HasPrefix(name, "ONE") || strings.HasPrefix(name, "TONE"), Timed: strings.HasPrefix(name, "T"), Nullable: false}
	if !req.Timed {
		if strings.TrimSpace(arg) != "" {
			v, err := vm.evalLooseExpr(arg)
			if err != nil {
				return execResult{}, err
			}
			def := v.Int64()
			if req.OneInput {
				if def >= 0 {
					req.HasDefault = true
					req.DefaultValue = Int(normalizeOneDigit(def))
				}
			} else {
				req.HasDefault = true
				req.DefaultValue = Int(def)
			}
		}
	} else {
		parts := splitTopLevelRuntime(arg, ',')
		if len(parts) == 1 {
			parts = strings.Fields(arg)
		}
		if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
			v, err := vm.evalLooseExpr(parts[0])
			if err == nil {
				req.TimeoutMs = v.Int64()
			}
		}
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			v, err := vm.evalLooseExpr(parts[1])
			if err == nil {
				def := v.Int64()
				if req.OneInput {
					def = normalizeOneDigit(def)
				}
				req.HasDefault = true
				req.DefaultValue = Int(def)
			}
		}
		req.Countdown = true
		if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
			if v, err := vm.evalLooseExpr(parts[2]); err == nil {
				req.Countdown = v.Int64() != 0
			}
		}
		if len(parts) > 3 && strings.TrimSpace(parts[3]) != "" {
			if v, err := vm.evalLooseExpr(parts[3]); err == nil {
				req.TimeoutMessage = v.String()
			} else {
				req.TimeoutMessage = decodeCommandCharSeq(strings.TrimSpace(parts[3]))
			}
		}
	}

	raw, _, err := vm.resolveInput(req)
	if err != nil {
		return execResult{}, err
	}
	var n int64
	if raw == "" {
		if req.HasDefault {
			n = req.DefaultValue.Int64()
		} else {
			n = 0
		}
	} else {
		parsed, ok := parseIntInput(raw)
		if !ok {
			if req.HasDefault {
				n = req.DefaultValue.Int64()
			} else {
				n = 0
			}
		} else {
			n = parsed
		}
	}
	if req.OneInput {
		n = normalizeOneDigit(n)
	}
	vm.globals["RESULT"] = Int(n)
	vm.maybeEchoInput(strconv.FormatInt(n, 10))
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execInputStringLike(name, arg string) (execResult, error) {
	req := InputRequest{Command: name, Numeric: false, OneInput: strings.HasPrefix(name, "ONE") || strings.HasPrefix(name, "TONE"), Timed: strings.HasPrefix(name, "T"), Nullable: false}
	if !req.Timed {
		if strings.TrimSpace(arg) != "" {
			v, err := vm.evalLooseExpr(arg)
			if err != nil {
				return execResult{}, err
			}
			def := v.String()
			if req.OneInput {
				def = firstRune(def)
			}
			req.HasDefault = true
			req.DefaultValue = Str(def)
		}
	} else {
		parts := splitTopLevelRuntime(arg, ',')
		if len(parts) == 1 {
			parts = strings.Fields(arg)
		}
		if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
			v, err := vm.evalLooseExpr(parts[0])
			if err == nil {
				req.TimeoutMs = v.Int64()
			}
		}
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			v, err := vm.evalLooseExpr(parts[1])
			if err == nil {
				def := v.String()
				if req.OneInput {
					def = firstRune(def)
				}
				req.HasDefault = true
				req.DefaultValue = Str(def)
			}
		}
		req.Countdown = true
		if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
			if v, err := vm.evalLooseExpr(parts[2]); err == nil {
				req.Countdown = v.Int64() != 0
			}
		}
		if len(parts) > 3 && strings.TrimSpace(parts[3]) != "" {
			if v, err := vm.evalLooseExpr(parts[3]); err == nil {
				req.TimeoutMessage = v.String()
			} else {
				req.TimeoutMessage = decodeCommandCharSeq(strings.TrimSpace(parts[3]))
			}
		}
	}

	raw, _, err := vm.resolveInput(req)
	if err != nil {
		return execResult{}, err
	}
	var out string
	if raw == "" {
		if req.HasDefault {
			out = req.DefaultValue.String()
		} else {
			out = ""
		}
	} else {
		out = raw
	}
	if req.OneInput {
		out = firstRune(out)
	}
	vm.globals["RESULTS"] = Str(out)
	vm.maybeEchoInput(out)
	return execResult{kind: resultNone}, nil
}
