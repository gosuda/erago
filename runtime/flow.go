package eruntime

import (
	"strings"

	"github.com/gosuda/erago/ast"
)

type thunkFlow struct {
	TryCatch     map[int]int
	CatchEnd     map[int]int
	TryListEnd   map[int]int
	TryListFuncs map[int][]string
}

type flowStackKind int

const (
	flowTryC flowStackKind = iota
	flowCatch
	flowTryList
)

type flowStackItem struct {
	kind flowStackKind
	idx  int
}

func (vm *VM) buildFlowIndex() {
	vm.flowMap = map[*ast.Thunk]*thunkFlow{}
	for _, fn := range vm.program.Functions {
		vm.buildFlowForThunk(fn.Body)
	}
}

func (vm *VM) buildFlowForThunk(thunk *ast.Thunk) {
	if thunk == nil {
		return
	}
	if _, exists := vm.flowMap[thunk]; exists {
		return
	}
	flow := &thunkFlow{
		TryCatch:     map[int]int{},
		CatchEnd:     map[int]int{},
		TryListEnd:   map[int]int{},
		TryListFuncs: map[int][]string{},
	}
	vm.flowMap[thunk] = flow

	stack := make([]flowStackItem, 0, 8)
	for i, stmt := range thunk.Statements {
		if cmd, ok := stmt.(ast.CommandStmt); ok {
			name := strings.ToUpper(strings.TrimSpace(cmd.Name))
			switch name {
			case "TRYCCALL", "TRYCCALLFORM", "TRYCJUMP", "TRYCJUMPFORM", "TRYCGOTO", "TRYCGOTOFORM":
				stack = append(stack, flowStackItem{kind: flowTryC, idx: i})
			case "CATCH":
				if len(stack) > 0 && stack[len(stack)-1].kind == flowTryC {
					pair := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					flow.TryCatch[pair.idx] = i
					stack = append(stack, flowStackItem{kind: flowCatch, idx: i})
				}
			case "ENDCATCH":
				if len(stack) > 0 && stack[len(stack)-1].kind == flowCatch {
					pair := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					flow.CatchEnd[pair.idx] = i
				}
			case "TRYCALLLIST", "TRYJUMPLIST", "TRYGOTOLIST":
				stack = append(stack, flowStackItem{kind: flowTryList, idx: i})
			case "FUNC":
				if len(stack) > 0 && stack[len(stack)-1].kind == flowTryList {
					base := stack[len(stack)-1].idx
					flow.TryListFuncs[base] = append(flow.TryListFuncs[base], strings.TrimSpace(cmd.Arg))
				}
			case "ENDFUNC":
				if len(stack) > 0 && stack[len(stack)-1].kind == flowTryList {
					base := stack[len(stack)-1].idx
					flow.TryListEnd[base] = i
					stack = stack[:len(stack)-1]
				}
			}
		}

		for _, child := range childThunks(stmt) {
			vm.buildFlowForThunk(child)
		}
	}
}

func childThunks(stmt ast.Statement) []*ast.Thunk {
	switch s := stmt.(type) {
	case ast.IfStmt:
		ret := make([]*ast.Thunk, 0, len(s.Branches)+1)
		for _, br := range s.Branches {
			ret = append(ret, br.Body)
		}
		ret = append(ret, s.Else)
		return ret
	case ast.SelectCaseStmt:
		ret := make([]*ast.Thunk, 0, len(s.Branches)+1)
		for _, br := range s.Branches {
			ret = append(ret, br.Body)
		}
		ret = append(ret, s.Else)
		return ret
	case ast.WhileStmt:
		return []*ast.Thunk{s.Body}
	case ast.DoWhileStmt:
		return []*ast.Thunk{s.Body}
	case ast.RepeatStmt:
		return []*ast.Thunk{s.Body}
	case ast.ForStmt:
		return []*ast.Thunk{s.Body}
	default:
		return nil
	}
}

func (vm *VM) currentFlow() *thunkFlow {
	if vm.execThunk == nil {
		return nil
	}
	return vm.flowMap[vm.execThunk]
}

func (vm *VM) currentCatchEndIndex() (int, bool) {
	flow := vm.currentFlow()
	if flow == nil {
		return 0, false
	}
	endIdx, ok := flow.CatchEnd[vm.execPC]
	if !ok {
		return 0, false
	}
	return endIdx, true
}

func (vm *VM) handleTryFailure(name string) (execResult, error) {
	if !strings.HasPrefix(name, "TRYC") {
		return execResult{kind: resultNone}, nil
	}
	flow := vm.currentFlow()
	if flow == nil {
		return execResult{kind: resultNone}, nil
	}
	catchIdx, ok := flow.TryCatch[vm.execPC]
	if !ok {
		return execResult{kind: resultNone}, nil
	}
	jump := catchIdx + 1
	if jump < 0 {
		jump = 0
	}
	return execResult{kind: resultJumpIndex, index: jump}, nil
}

func (vm *VM) execTryListBlock(name string) (execResult, bool, error) {
	flow := vm.currentFlow()
	if flow == nil {
		return execResult{}, false, nil
	}
	entries, ok := flow.TryListFuncs[vm.execPC]
	if !ok {
		return execResult{}, false, nil
	}
	endIdx, hasEnd := flow.TryListEnd[vm.execPC]

	skipToEnd := func() (execResult, bool, error) {
		if hasEnd {
			return execResult{kind: resultJumpIndex, index: endIdx}, true, nil
		}
		return execResult{kind: resultNone}, true, nil
	}

	if name == "TRYGOTOLIST" {
		fr := vm.currentFrame()
		if fr == nil {
			return skipToEnd()
		}
		for _, raw := range entries {
			label, err := vm.evalCommandTarget(raw, false)
			if err != nil || strings.TrimSpace(label) == "" {
				continue
			}
			if _, ok := fr.fn.Body.LabelMap[strings.ToUpper(label)]; ok {
				return execResult{kind: resultGoto, label: strings.ToUpper(label)}, true, nil
			}
		}
		return skipToEnd()
	}

	for _, raw := range entries {
		target, args, err := vm.parseCommandCall(raw, false)
		if err != nil || strings.TrimSpace(target) == "" {
			continue
		}
		target = strings.ToUpper(strings.TrimSpace(target))
		if vm.program.Functions[target] == nil {
			continue
		}
		res, err := vm.callFunction(target, args)
		if err != nil {
			return execResult{}, true, err
		}
		if hasEnd && res.kind == resultNone {
			return execResult{kind: resultJumpIndex, index: endIdx}, true, nil
		}
		return res, true, nil
	}
	return skipToEnd()
}
