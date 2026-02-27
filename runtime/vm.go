package eruntime

import (
	"fmt"
	"hash/fnv"
	"html"
	"math"
	"math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gosuda/erago/ast"
	"github.com/gosuda/erago/parser"
)

type Output struct {
	Text    string
	NewLine bool
}

type VM struct {
	program        *ast.Program
	globals        map[string]Value
	gArrays        map[string]*ArrayVar
	gRefDecl       map[string]bool
	gRefs          map[string]ast.VarRef
	stack          []*frame
	outputs        []Output
	rng            *rand.Rand
	csv            *CSVStore
	saveDir        string
	ui             UIState
	characters     []RuntimeCharacter
	nextCharID     int64
	flowMap        map[*ast.Thunk]*thunkFlow
	execThunk      *ast.Thunk
	execPC         int
	input          InputState
	saveUniqueCode int64
	saveVersion    int64
	datSaveFormat  string
	outputHook     func(Output)
	inputProvider  func(InputRequest) (string, bool, error)
}

type frame struct {
	fn       *ast.Function
	locals   map[string]Value
	lArrays  map[string]*ArrayVar
	lRefDecl map[string]bool
	refs     map[string]ast.VarRef
}

type resultKind int

const (
	resultNone resultKind = iota
	resultGoto
	resultJumpIndex
	resultBegin
	resultReturn
	resultQuit
	resultBreak
	resultContinue
)

type execResult struct {
	kind    resultKind
	index   int
	label   string
	keyword string
	values  []Value
}

var htmlTagPattern = regexp.MustCompile(`(?is)<[^>]*>`)

func New(program *ast.Program) (*VM, error) {
	vm := &VM{
		program:        program,
		globals:        map[string]Value{},
		gArrays:        map[string]*ArrayVar{},
		gRefDecl:       map[string]bool{},
		gRefs:          map[string]ast.VarRef{},
		stack:          nil,
		outputs:        nil,
		rng:            rand.New(rand.NewSource(time.Now().UnixNano())),
		csv:            newCSVStore(program.CSVFiles),
		saveDir:        "",
		ui:             defaultUIState(),
		characters:     nil,
		nextCharID:     0,
		flowMap:        map[*ast.Thunk]*thunkFlow{},
		execThunk:      nil,
		execPC:         -1,
		input:          defaultInputState(),
		saveUniqueCode: 0,
		saveVersion:    1,
		datSaveFormat:  "json",
		outputHook:     nil,
		inputProvider:  nil,
	}
	vm.initSaveIdentity()
	if err := vm.initDefines(); err != nil {
		return nil, err
	}
	vm.buildFlowIndex()
	return vm, nil
}

func (vm *VM) initDefines() error {
	keys := make([]string, 0, len(vm.program.Defines))
	for k := range vm.program.Defines {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	indexedKeys := make([]string, 0, len(keys))
	for _, k := range keys {
		if strings.Contains(k, ":") {
			indexedKeys = append(indexedKeys, k)
			continue
		}
		v, err := vm.evalExpr(vm.program.Defines[k])
		if err != nil {
			vm.globals[k] = Int(0)
			continue
		}
		vm.globals[k] = v
	}
	for _, decl := range vm.program.VarDecls {
		name := strings.ToUpper(strings.TrimSpace(decl.Name))
		if name == "" {
			continue
		}
		if decl.Scope != "global" {
			continue
		}
		if decl.IsRef {
			vm.gRefDecl[name] = true
			continue
		}
		vm.gArrays[name] = newArrayVar(decl.IsString, decl.IsDynamic, decl.Dims)
	}
	for _, k := range indexedKeys {
		expr, err := parser.ParseExpr(k)
		if err != nil {
			continue
		}
		ref, ok := expr.(ast.VarRef)
		if !ok || len(ref.Index) == 0 {
			continue
		}
		v, err := vm.evalExpr(vm.program.Defines[k])
		if err != nil {
			continue
		}
		if err := vm.setVarRef(ref, v); err != nil {
			return fmt.Errorf("init %s: %w", k, err)
		}
	}
	for name := range vm.program.StringVars {
		if _, ok := vm.globals[name]; !ok && vm.gArrays[name] == nil {
			vm.globals[name] = Str("")
		}
	}
	title, author, year, windowTitle, info := vm.csv.GameMeta()
	if strings.TrimSpace(title) != "" {
		vm.globals["GAMEBASE_TITLE"] = Str(title)
	}
	if strings.TrimSpace(author) != "" {
		vm.globals["GAMEBASE_AUTHOR"] = Str(author)
	}
	if strings.TrimSpace(year) != "" {
		vm.globals["GAMEBASE_YEAR"] = Str(year)
	}
	if strings.TrimSpace(windowTitle) != "" {
		vm.globals["GAMEBASE_WINDOWTITLE"] = Str(windowTitle)
	}
	if strings.TrimSpace(info) != "" {
		vm.globals["GAMEBASE_INFO"] = Str(info)
	}
	if _, ok := vm.globals["GAMEBASE_VERSION"]; !ok {
		if _, version, _, hasVersion := vm.csv.GameCodeVersion(); hasVersion {
			vm.globals["GAMEBASE_VERSION"] = Int(version)
		}
	}
	if _, ok := vm.globals["RESULT"]; !ok {
		vm.globals["RESULT"] = Int(0)
	}
	return nil
}

func (vm *VM) initSaveIdentity() {
	code, version, hasCode, hasVersion := vm.csv.GameCodeVersion()
	if hasCode {
		vm.saveUniqueCode = code
	} else {
		h := fnv.New64a()
		keys := make([]string, 0, len(vm.program.Functions))
		for name := range vm.program.Functions {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_, _ = h.Write([]byte(k))
			fn := vm.program.Functions[k]
			if fn == nil || fn.Body == nil {
				continue
			}
			for _, st := range fn.Body.Statements {
				switch s := st.(type) {
				case ast.CommandStmt:
					_, _ = h.Write([]byte(strings.ToUpper(strings.TrimSpace(s.Name))))
					_, _ = h.Write([]byte(strings.TrimSpace(s.Arg)))
				case ast.AssignStmt:
					_, _ = h.Write([]byte(strings.ToUpper(strings.TrimSpace(s.Target.Name))))
				}
			}
		}
		vm.saveUniqueCode = int64(h.Sum64() & 0x7fffffffffffffff)
	}
	if hasVersion {
		vm.saveVersion = version
	} else {
		vm.saveVersion = 1
	}
}

func (vm *VM) Run(entry string) ([]Output, error) {
	queuedInput := append([]string(nil), vm.input.Queue...)
	vm.outputs = vm.outputs[:0]
	vm.ui = defaultUIState()
	vm.characters = nil
	vm.nextCharID = 0
	vm.input = defaultInputState()
	vm.input.Queue = queuedInput
	vm.refreshCharacterGlobals()
	current := strings.ToUpper(strings.TrimSpace(entry))
	if current == "" {
		current = "TITLE"
	}
	for {
		res, err := vm.callFunction(current, nil)
		if err != nil {
			return nil, err
		}
		switch res.kind {
		case resultBegin:
			current = vm.resolveBeginTarget(res.keyword)
			continue
		case resultQuit:
			return append([]Output(nil), vm.outputs...), nil
		case resultGoto:
			return nil, fmt.Errorf("uncaught goto %s", res.label)
		default:
			return append([]Output(nil), vm.outputs...), nil
		}
	}
}

func (vm *VM) resolveBeginTarget(keyword string) string {
	kw := strings.ToUpper(strings.TrimSpace(keyword))
	candidates := []string{kw}
	switch kw {
	case "TITLE":
		candidates = append(candidates, "SYSTEM_TITLE")
	case "FIRST":
		candidates = append(candidates, "EVENTFIRST")
	case "SHOP":
		candidates = append(candidates, "EVENTSHOP")
	case "TRAIN":
		candidates = append(candidates, "EVENTTRAIN")
	case "AFTERTRAIN", "AFTERTRA", "END":
		candidates = append(candidates, "EVENTEND")
	case "TURNEND":
		candidates = append(candidates, "EVENTTURNEND")
	case "COM":
		candidates = append(candidates, "EVENTCOM")
	case "LOAD":
		candidates = append(candidates, "EVENTLOAD")
	}
	for _, c := range candidates {
		if vm.program.Functions[c] != nil {
			return c
		}
	}
	return kw
}

func (vm *VM) Globals() map[string]Value {
	cp := make(map[string]Value, len(vm.globals))
	for k, v := range vm.globals {
		cp[k] = v
	}
	return cp
}

func (vm *VM) SetSaveDir(dir string) {
	vm.saveDir = dir
}

func (vm *VM) SetOutputHook(hook func(Output)) {
	vm.outputHook = hook
}

func (vm *VM) SetInputProvider(provider func(InputRequest) (string, bool, error)) {
	vm.inputProvider = provider
}

func (vm *VM) SetDatSaveFormat(format string) error {
	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "json", "binary", "both":
		vm.datSaveFormat = format
		return nil
	default:
		return fmt.Errorf("invalid dat save format %q (use json|binary|both)", format)
	}
}

func (vm *VM) DatSaveFormat() string {
	return vm.datSaveFormat
}

func (vm *VM) emitOutput(out Output) {
	vm.outputs = append(vm.outputs, out)
	if vm.outputHook != nil {
		vm.outputHook(out)
	}
}

func (vm *VM) callFunction(name string, args []Value) (execResult, error) {
	name = strings.ToUpper(name)
	fn := vm.program.Functions[name]
	if fn == nil {
		return execResult{}, fmt.Errorf("function %s not found", name)
	}
	fr := &frame{
		fn:       fn,
		locals:   map[string]Value{},
		lArrays:  map[string]*ArrayVar{},
		lRefDecl: map[string]bool{},
		refs:     map[string]ast.VarRef{},
	}
	vm.stack = append(vm.stack, fr)
	defer func() {
		vm.stack = vm.stack[:len(vm.stack)-1]
	}()

	for i, arg := range fn.Args {
		target := vm.normalizeFuncArgTarget(arg, i)

		assignArg := func(v Value) error {
			if len(target.Index) == 0 {
				fr.locals[target.Name] = v
				return nil
			}
			index, err := vm.evalIndexExprsFor(target.Name, target.Index)
			if err != nil {
				return err
			}
			arr := fr.lArrays[target.Name]
			if arr == nil {
				arr = newArrayVar(v.Kind() == StringKind, true, dimsForIndex(index))
				fr.lArrays[target.Name] = arr
			}
			if v.Kind() == StringKind {
				arr.IsString = true
			}
			return arr.Set(index, v)
		}

		if i < len(args) {
			if err := assignArg(args[i]); err != nil {
				return execResult{}, fmt.Errorf("%s arg %s: %w", fn.Name, arg.Name, err)
			}
			continue
		}
		if arg.Default != nil {
			v, err := vm.evalExpr(arg.Default)
			if err != nil {
				return execResult{}, fmt.Errorf("%s default arg %s: %w", fn.Name, arg.Name, err)
			}
			if err := assignArg(v); err != nil {
				return execResult{}, fmt.Errorf("%s default arg %s: %w", fn.Name, arg.Name, err)
			}
			continue
		}
		if err := assignArg(vm.defaultValueForFuncArgTarget(target)); err != nil {
			return execResult{}, fmt.Errorf("%s arg %s: %w", fn.Name, arg.Name, err)
		}
	}

	for _, decl := range fn.VarDecls {
		name := strings.ToUpper(strings.TrimSpace(decl.Name))
		if name == "" {
			continue
		}
		switch decl.Scope {
		case "global":
			if decl.IsRef {
				vm.gRefDecl[name] = true
				continue
			}
			if vm.gArrays[name] == nil {
				vm.gArrays[name] = newArrayVar(decl.IsString, decl.IsDynamic, decl.Dims)
			}
		default:
			if decl.IsRef {
				fr.lRefDecl[name] = true
				continue
			}
			fr.lArrays[name] = newArrayVar(decl.IsString, decl.IsDynamic, decl.Dims)
		}
	}

	res, err := vm.runThunk(fn.Body)
	if err != nil {
		return execResult{}, err
	}
	if res.kind == resultReturn {
		vm.storeResult(res.values)
		return execResult{kind: resultNone}, nil
	}
	return res, nil
}

func (vm *VM) runThunk(thunk *ast.Thunk) (execResult, error) {
	prevThunk := vm.execThunk
	prevPC := vm.execPC
	vm.execThunk = thunk
	vm.execPC = -1
	defer func() {
		vm.execThunk = prevThunk
		vm.execPC = prevPC
	}()

	for pc := 0; pc < len(thunk.Statements); pc++ {
		vm.execPC = pc
		stmt := thunk.Statements[pc]
		res, err := vm.runStatement(stmt)
		if err != nil {
			fnName := ""
			if fr := vm.currentFrame(); fr != nil && fr.fn != nil {
				fnName = fr.fn.Name
			}
			if fnName == "" {
				return execResult{}, fmt.Errorf("pc %d (%T): %w", pc, stmt, err)
			}
			return execResult{}, fmt.Errorf("%s pc %d (%T): %w", fnName, pc, stmt, err)
		}
		if res.kind == resultGoto {
			idx, ok := thunk.LabelMap[strings.ToUpper(res.label)]
			if ok {
				pc = idx - 1
				continue
			}
			return res, nil
		}
		if res.kind == resultJumpIndex {
			if res.index >= 0 && res.index < len(thunk.Statements) {
				pc = res.index - 1
				continue
			}
			return execResult{}, fmt.Errorf("invalid jump index %d", res.index)
		}
		if res.kind != resultNone {
			return res, nil
		}
	}
	return execResult{kind: resultNone}, nil
}

func (vm *VM) runStatement(stmt ast.Statement) (execResult, error) {
	switch s := stmt.(type) {
	case ast.PrintStmt:
		if vm.ui.SkipDisp {
			return execResult{kind: resultNone}, nil
		}
		v, err := vm.evalExpr(s.Expr)
		if err != nil {
			return execResult{}, err
		}
		vm.emitOutput(Output{Text: v.String(), NewLine: s.NewLine})
		return execResult{kind: resultNone}, nil
	case ast.AssignStmt:
		if s.Op == "=" && len(s.Target.Index) == 0 {
			if sourceRef, ok := s.Expr.(ast.VarRef); ok && vm.isRefDeclared(s.Target.Name) {
				vm.setRefBinding(strings.ToUpper(s.Target.Name), sourceRef)
				return execResult{kind: resultNone}, nil
			}
		}
		var v Value
		var err error
		if _, empty := s.Expr.(ast.EmptyLit); empty {
			v, err = vm.defaultValueForVarRef(s.Target)
		} else {
			v, err = vm.evalExpr(s.Expr)
		}
		if err != nil {
			return execResult{}, err
		}
		if _, isStringExpr := s.Expr.(ast.StringLit); isStringExpr && v.Kind() == StringKind {
			raw := v.String()
			if strings.Contains(raw, "%") || strings.Contains(raw, "{") || strings.Contains(raw, "@") {
				expanded, err := vm.expandFormTemplate(raw)
				if err != nil {
					return execResult{}, err
				}
				v = Str(expanded)
			}
		}
		current, err := vm.getVarRef(s.Target)
		if err != nil {
			return execResult{}, err
		}
		if s.Op == "=" {
			if err := vm.setVarRef(s.Target, v); err != nil {
				return execResult{}, err
			}
			return execResult{kind: resultNone}, nil
		}
		next, err := evalAssignBinary(s.Op, current, v)
		if err != nil {
			return execResult{}, err
		}
		if err := vm.setVarRef(s.Target, next); err != nil {
			return execResult{}, err
		}
		return execResult{kind: resultNone}, nil
	case ast.IncDecStmt:
		current, err := vm.getVarRef(s.Target)
		if err != nil {
			return execResult{}, err
		}
		delta := int64(1)
		if s.Op == "--" {
			delta = -1
		}
		if err := vm.setVarRef(s.Target, Int(current.Int64()+delta)); err != nil {
			return execResult{}, err
		}
		return execResult{kind: resultNone}, nil
	case ast.IfStmt:
		for _, br := range s.Branches {
			cond, err := vm.evalExpr(br.Cond)
			if err != nil {
				return execResult{}, err
			}
			if cond.Truthy() {
				return vm.runThunk(br.Body)
			}
		}
		return vm.runThunk(s.Else)
	case ast.SelectCaseStmt:
		target, err := vm.evalExpr(s.Target)
		if err != nil {
			return execResult{}, err
		}
		for _, br := range s.Branches {
			ok, err := vm.matchCaseConditions(target, br.Conditions)
			if err != nil {
				return execResult{}, err
			}
			if ok {
				return vm.runThunk(br.Body)
			}
		}
		return vm.runThunk(s.Else)
	case ast.WhileStmt:
		for {
			cond, err := vm.evalExpr(s.Cond)
			if err != nil {
				return execResult{}, err
			}
			if !cond.Truthy() {
				return execResult{kind: resultNone}, nil
			}
			res, err := vm.runThunk(s.Body)
			if err != nil {
				return execResult{}, err
			}
			switch res.kind {
			case resultNone:
				continue
			case resultContinue:
				continue
			case resultBreak:
				return execResult{kind: resultNone}, nil
			default:
				return res, nil
			}
		}
	case ast.DoWhileStmt:
		for {
			res, err := vm.runThunk(s.Body)
			if err != nil {
				return execResult{}, err
			}
			switch res.kind {
			case resultNone:
			case resultContinue:
			case resultBreak:
				return execResult{kind: resultNone}, nil
			default:
				return res, nil
			}
			cond, err := vm.evalExpr(s.Cond)
			if err != nil {
				return execResult{}, err
			}
			if !cond.Truthy() {
				return execResult{kind: resultNone}, nil
			}
		}
	case ast.RepeatStmt:
		count, err := vm.evalExpr(s.Count)
		if err != nil {
			return execResult{}, err
		}
		n := count.Int64()
		if n < 0 {
			n = 0
		}
		for i := int64(0); i < n; i++ {
			res, err := vm.runThunk(s.Body)
			if err != nil {
				return execResult{}, err
			}
			switch res.kind {
			case resultNone:
			case resultContinue:
				continue
			case resultBreak:
				return execResult{kind: resultNone}, nil
			default:
				return res, nil
			}
		}
		return execResult{kind: resultNone}, nil
	case ast.ForStmt:
		initVal, err := vm.evalExpr(s.Init)
		if err != nil {
			return execResult{}, err
		}
		limitVal, err := vm.evalExpr(s.Limit)
		if err != nil {
			return execResult{}, err
		}
		stepVal, err := vm.evalExpr(s.Step)
		if err != nil {
			return execResult{}, err
		}
		target := s.Target
		if strings.TrimSpace(target.Name) == "" {
			target = ast.VarRef{Name: strings.ToUpper(s.Var)}
		}
		target.Name = strings.ToUpper(strings.TrimSpace(target.Name))
		step := stepVal.Int64()
		if step == 0 {
			step = 1
		}
		if err := vm.setVarRef(target, Int(initVal.Int64())); err != nil {
			return execResult{}, err
		}
		for {
			curVal, err := vm.getVarRef(target)
			if err != nil {
				return execResult{}, err
			}
			cur := curVal.Int64()
			limit := limitVal.Int64()
			if (step > 0 && cur >= limit) || (step < 0 && cur <= limit) {
				return execResult{kind: resultNone}, nil
			}

			res, err := vm.runThunk(s.Body)
			if err != nil {
				return execResult{}, err
			}
			switch res.kind {
			case resultNone:
			case resultContinue:
			case resultBreak:
				return execResult{kind: resultNone}, nil
			default:
				return res, nil
			}
			if err := vm.setVarRef(target, Int(cur+step)); err != nil {
				return execResult{}, err
			}
		}
	case ast.GotoStmt:
		return execResult{kind: resultGoto, label: strings.ToUpper(s.Label)}, nil
	case ast.CallStmt:
		args := make([]Value, 0, len(s.Args))
		for _, e := range s.Args {
			v, err := vm.evalCallArgExpr(e)
			if err != nil {
				return execResult{}, err
			}
			args = append(args, v)
		}
		res, err := vm.callFunction(s.Name, args)
		if err != nil {
			return execResult{}, err
		}
		if res.kind == resultReturn {
			return execResult{kind: resultNone}, nil
		}
		return res, nil
	case ast.ReturnStmt:
		vals := make([]Value, 0, len(s.Values))
		for _, e := range s.Values {
			v, err := vm.evalExpr(e)
			if err != nil {
				return execResult{}, err
			}
			vals = append(vals, v)
		}
		return execResult{kind: resultReturn, values: vals}, nil
	case ast.BeginStmt:
		return execResult{kind: resultBegin, keyword: s.Keyword}, nil
	case ast.QuitStmt:
		return execResult{kind: resultQuit}, nil
	case ast.BreakStmt:
		return execResult{kind: resultBreak}, nil
	case ast.ContinueStmt:
		return execResult{kind: resultContinue}, nil
	case ast.CommandStmt:
		return vm.runCommandStatement(s)
	case ast.PrintDataStmt:
		text, err := vm.pickDataItemText(s.Items)
		if err != nil {
			return execResult{}, err
		}
		vm.emitOutput(Output{
			Text:    text,
			NewLine: shouldNewlineOnPrint(s.Command),
		})
		return execResult{kind: resultNone}, nil
	case ast.StrDataStmt:
		text, err := vm.pickDataItemText(s.Items)
		if err != nil {
			return execResult{}, err
		}
		if err := vm.setVarRef(s.Target, Str(text)); err != nil {
			return execResult{}, err
		}
		return execResult{kind: resultNone}, nil
	default:
		return execResult{}, fmt.Errorf("unsupported statement %T", stmt)
	}
}

func (vm *VM) matchCaseConditions(target Value, conditions []ast.CaseCondition) (bool, error) {
	for _, cond := range conditions {
		switch cond.Kind {
		case "equal":
			v, err := vm.evalExpr(cond.Expr)
			if err != nil {
				return false, err
			}
			if valueEqual(target, v) {
				return true, nil
			}
		case "range":
			from, err := vm.evalExpr(cond.From)
			if err != nil {
				return false, err
			}
			to, err := vm.evalExpr(cond.To)
			if err != nil {
				return false, err
			}
			tv := target.Int64()
			if from.Int64() <= tv && tv <= to.Int64() {
				return true, nil
			}
		case "compare":
			v, err := vm.evalExpr(cond.Expr)
			if err != nil {
				return false, err
			}
			t := target.Int64()
			r := v.Int64()
			switch cond.Op {
			case "<":
				if t < r {
					return true, nil
				}
			case "<=":
				if t <= r {
					return true, nil
				}
			case ">":
				if t > r {
					return true, nil
				}
			case ">=":
				if t >= r {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (vm *VM) pickDataItemText(items []ast.DataItem) (string, error) {
	if len(items) == 0 {
		return "", nil
	}
	idx := vm.rng.Intn(len(items))
	item := items[idx]
	switch item.Kind {
	case "dataform":
		return vm.evalPrintForm(item.Raw)
	case "data":
		return decodeCommandCharSeq(item.Raw), nil
	default:
		return item.Raw, nil
	}
}

func valueEqual(a, b Value) bool {
	if a.Kind() == StringKind || b.Kind() == StringKind {
		return a.String() == b.String()
	}
	return a.Int64() == b.Int64()
}

func (vm *VM) runCommandStatement(s ast.CommandStmt) (execResult, error) {
	name := strings.ToUpper(strings.TrimSpace(s.Name))
	arg := strings.TrimSpace(s.Arg)

	if strings.HasPrefix(name, "PRINT") || strings.HasPrefix(name, "DEBUGPRINT") {
		text, err := vm.evalCommandPrint(name, arg)
		if err != nil {
			return execResult{}, err
		}
		if !vm.ui.SkipDisp {
			vm.emitOutput(Output{Text: text, NewLine: shouldNewlineOnPrint(name)})
		}
		return execResult{kind: resultNone}, nil
	}

	switch name {
	case "WAIT", "WAITANYKEY", "FORCEWAIT", "TWAIT", "AWAIT":
		return vm.execWaitLike(name, arg)
	case "INPUT", "ONEINPUT", "TINPUT", "TONEINPUT":
		return vm.execInputIntLike(name, arg)
	case "INPUTS", "ONEINPUTS", "TINPUTS", "TONEINPUTS":
		return vm.execInputStringLike(name, arg)
	case "GETTIME":
		vm.globals["RESULT"] = Int(time.Now().Unix())
		return execResult{kind: resultNone}, nil
	case "GETSECOND":
		vm.globals["RESULT"] = Int(int64(time.Now().Second()))
		return execResult{kind: resultNone}, nil
	case "GETMILLISECOND":
		vm.globals["RESULT"] = Int(int64(time.Now().Nanosecond() / 1e6))
		return execResult{kind: resultNone}, nil
	case "RANDOMIZE":
		vm.rng.Seed(time.Now().UnixNano())
		return execResult{kind: resultNone}, nil
	case "INITRAND":
		if arg != "" {
			v, err := vm.evalLooseExpr(arg)
			if err == nil {
				vm.rng.Seed(v.Int64())
			}
		}
		return execResult{kind: resultNone}, nil
	case "DUMPRAND":
		vm.globals["RESULT"] = Int(vm.rng.Int63())
		return execResult{kind: resultNone}, nil
	case "RESTART":
		return execResult{kind: resultBegin, keyword: "TITLE"}, nil
	case "THROW":
		if arg == "" {
			return execResult{}, fmt.Errorf("THROW without message")
		}
		v, err := vm.evalLooseExpr(arg)
		if err != nil {
			return execResult{}, err
		}
		return execResult{}, fmt.Errorf("THROW: %s", v.String())
	case "RETURNFORM":
		text, err := vm.evalPrintForm(arg)
		if err != nil {
			return execResult{}, err
		}
		return execResult{kind: resultReturn, values: []Value{Str(text)}}, nil
	case "RETURNF":
		values, err := vm.evalExprList(arg)
		if err != nil {
			return execResult{}, err
		}
		for i := range values {
			if values[i].Kind() != StringKind {
				continue
			}
			expanded, err := vm.expandFormTemplate(values[i].String())
			if err != nil {
				return execResult{}, err
			}
			values[i] = Str(expanded)
		}
		return execResult{kind: resultReturn, values: values}, nil
	case "BEGIN":
		if arg == "" {
			return execResult{}, fmt.Errorf("BEGIN without keyword")
		}
		return execResult{kind: resultBegin, keyword: strings.ToUpper(arg)}, nil
	case "QUIT":
		return execResult{kind: resultQuit}, nil
	case "SAVEDATA":
		return vm.execSaveData(arg)
	case "LOADDATA":
		return vm.execLoadData(arg)
	case "DELDATA":
		return vm.execDeleteData(arg)
	case "CHKDATA":
		return vm.execCheckData(arg)
	case "SAVEGAME":
		return vm.execSaveGame(arg)
	case "LOADGAME":
		return vm.execLoadGame(arg)
	case "SAVEGLOBAL":
		if err := vm.saveGlobals("global"); err != nil {
			return execResult{}, err
		}
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "LOADGLOBAL":
		ok, err := vm.loadGlobals("global")
		if err != nil {
			return execResult{}, err
		}
		if ok {
			vm.globals["RESULT"] = Int(1)
		} else {
			vm.globals["RESULT"] = Int(0)
		}
		return execResult{kind: resultNone}, nil
	case "VARSET", "CVARSET":
		return vm.execVarSet(arg)
	case "TIMES":
		return vm.execTimes(arg)
	case "SPLIT":
		return vm.execSplit(arg)
	case "ESCAPE":
		return vm.execEscape(arg)
	case "ENCODETOUNI":
		return vm.execEncodeToUni(arg)
	case "PUTFORM":
		return vm.execPutForm(arg)
	case "BAR", "BARL":
		return vm.execBar(name, arg)
	case "SETBIT", "CLEARBIT", "INVERTBIT":
		return vm.execBitMutation(name, arg)
	case "GETBIT":
		return vm.execGetBit(arg)
	case "SWAP":
		return vm.execSwap(arg)
	case "ARRAYSHIFT":
		return vm.execArrayShift(arg)
	case "ARRAYREMOVE":
		return vm.execArrayRemove(arg)
	case "ARRAYCOPY":
		return vm.execArrayCopy(arg)
	case "ARRAYSORT":
		return vm.execArraySort(arg)
	case "DRAWLINE", "CUSTOMDRAWLINE", "DRAWLINEFORM":
		return vm.execDrawLine(name, arg)
	case "CLEARLINE":
		return vm.execClearLine(arg)
	case "REUSELASTLINE":
		return vm.execReuseLastLine()
	case "ALIGNMENT":
		return vm.execAlignment(arg)
	case "CURRENTALIGN":
		vm.globals["RESULT"] = Str(vm.ui.Align)
		return execResult{kind: resultNone}, nil
	case "REDRAW":
		return vm.execRedraw(arg)
	case "CURRENTREDRAW":
		if vm.ui.Redraw {
			vm.globals["RESULT"] = Int(1)
		} else {
			vm.globals["RESULT"] = Int(0)
		}
		return execResult{kind: resultNone}, nil
	case "SKIPDISP", "MOUSESKIP", "NOSKIP", "ENDNOSKIP":
		return vm.execSkipDisp(arg)
	case "ISSKIP":
		if vm.ui.SkipDisp {
			vm.globals["RESULT"] = Int(1)
		} else {
			vm.globals["RESULT"] = Int(0)
		}
		return execResult{kind: resultNone}, nil
	case "SETCOLOR", "SETCOLORBYNAME":
		return vm.execSetColor(arg)
	case "SETBGCOLOR", "SETBGCOLORBYNAME":
		return vm.execSetBgColor(arg)
	case "RESETCOLOR":
		vm.ui.Color = "FFFFFF"
		return execResult{kind: resultNone}, nil
	case "RESETBGCOLOR":
		vm.ui.BgColor = "000000"
		return execResult{kind: resultNone}, nil
	case "GETCOLOR":
		vm.globals["RESULT"] = Str(vm.ui.Color)
		return execResult{kind: resultNone}, nil
	case "GETBGCOLOR":
		vm.globals["RESULT"] = Str(vm.ui.BgColor)
		return execResult{kind: resultNone}, nil
	case "GETDEFCOLOR":
		vm.globals["RESULT"] = Str("FFFFFF")
		return execResult{kind: resultNone}, nil
	case "GETDEFBGCOLOR":
		vm.globals["RESULT"] = Str("000000")
		return execResult{kind: resultNone}, nil
	case "GETFOCUSCOLOR":
		vm.globals["RESULT"] = Str(vm.ui.FocusColor)
		return execResult{kind: resultNone}, nil
	case "SETFONT":
		vm.ui.Font = strings.TrimSpace(arg)
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "GETFONT":
		vm.globals["RESULT"] = Str(vm.ui.Font)
		return execResult{kind: resultNone}, nil
	case "CHKFONT":
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "FONTBOLD":
		vm.ui.Bold = true
		return execResult{kind: resultNone}, nil
	case "FONTITALIC":
		vm.ui.Italic = true
		return execResult{kind: resultNone}, nil
	case "FONTREGULAR":
		vm.ui.Bold = false
		vm.ui.Italic = false
		return execResult{kind: resultNone}, nil
	case "FONTSTYLE":
		return vm.execFontStyle(arg)
	case "PRINTCPERLINE":
		return vm.execPrintCPerLine(arg)
	case "ADDCHARA", "ADDDEFCHARA", "ADDVOIDCHARA", "ADDSPCHARA":
		return vm.execAddChara(arg)
	case "DELCHARA":
		return vm.execDelChara(arg)
	case "DELALLCHARA":
		vm.characters = nil
		vm.refreshCharacterGlobals()
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "GETCHARA":
		return vm.execGetChara(arg)
	case "FINDCHARA":
		return vm.execFindChara(arg, false)
	case "FINDLASTCHARA":
		return vm.execFindChara(arg, true)
	case "SWAPCHARA":
		return vm.execSwapChara(arg)
	case "SORTCHARA":
		vm.sortCharacters()
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "COPYCHARA":
		return vm.execCopyChara(arg, false)
	case "ADDCOPYCHARA":
		return vm.execCopyChara(arg, true)
	case "PICKUPCHARA":
		return vm.execPickupChara(arg)
	case "ISACTIVE":
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "MOUSEX", "MOUSEY":
		vm.globals["RESULT"] = Int(0)
		return execResult{kind: resultNone}, nil
	case "OUTPUTLOG", "SAVENOS":
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "DEBUGCLEAR":
		vm.outputs = vm.outputs[:0]
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "ASSERT":
		if strings.TrimSpace(arg) == "" {
			return execResult{}, fmt.Errorf("ASSERT without expression")
		}
		v, err := vm.evalLooseExpr(arg)
		if err != nil {
			return execResult{}, err
		}
		if !v.Truthy() {
			return execResult{}, fmt.Errorf("ASSERT failed")
		}
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "REF", "REFBYNAME":
		return vm.execRefBinding(name, arg)
	case "RESETGLOBAL":
		return vm.execResetGlobal()
	case "RESETDATA":
		return vm.execResetData()
	case "CATCH":
		if endIdx, ok := vm.currentCatchEndIndex(); ok {
			vm.globals["RESULT"] = Int(1)
			return execResult{kind: resultJumpIndex, index: endIdx}, nil
		}
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "ENDCATCH", "FUNC", "ENDFUNC":
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "HTML_PRINT":
		outText := ""
		if strings.TrimSpace(arg) != "" {
			if v, err := vm.evalLooseExpr(arg); err == nil {
				outText = v.String()
			} else {
				outText = decodeCommandCharSeq(arg)
				if s, ok := tryUnquoteCommandString(outText); ok {
					outText = s
				}
			}
		}
		for pass := 0; pass < 6; pass++ {
			prev := outText
			if t, err := vm.evalPercentPlaceholders(outText); err == nil {
				outText = t
			}
			if t, err := vm.evalBracePlaceholders(outText); err == nil {
				outText = t
			}
			if outText == prev {
				break
			}
		}
		outText = html.UnescapeString(outText)
		outText = htmlTagPattern.ReplaceAllString(outText, "")
		if strings.TrimSpace(outText) != "" {
			vm.emitOutput(Output{Text: outText, NewLine: true})
		}
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "RESET_STAIN", "STOPCALLTRAIN", "CBGCLEAR", "CBGCLEARBUTTON", "CBGREMOVEBMAP", "CLEARTEXTBOX", "UPCHECK", "CUPCHECK", "DOTRAIN", "FORCEKANA", "HTML_TAGSPLIT", "INPUTMOUSEKEY", "TOOLTIP_SETCOLOR", "TOOLTIP_SETDELAY", "TOOLTIP_SETDURATION":
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "SAVEVAR":
		return vm.execSaveVar(arg)
	case "LOADVAR":
		return vm.execLoadVar(arg)
	case "SAVECHARA":
		return vm.execSaveChara(arg)
	case "LOADCHARA":
		return vm.execLoadChara(arg)
	case "GOTO", "GOTOFORM", "TRYGOTO", "TRYGOTOFORM", "TRYCGOTO", "TRYCGOTOFORM":
		label, err := vm.evalCommandTarget(arg, strings.Contains(name, "FORM"))
		if err != nil {
			if strings.HasPrefix(name, "TRY") {
				return vm.handleTryFailure(name)
			}
			return execResult{}, err
		}
		if label == "" {
			if strings.HasPrefix(name, "TRY") {
				return vm.handleTryFailure(name)
			}
			return execResult{}, fmt.Errorf("%s without target", name)
		}
		if strings.HasPrefix(name, "TRY") {
			fr := vm.currentFrame()
			if fr != nil {
				if _, ok := fr.fn.Body.LabelMap[strings.ToUpper(label)]; !ok {
					return vm.handleTryFailure(name)
				}
			}
		}
		return execResult{kind: resultGoto, label: strings.ToUpper(label)}, nil
	}

	if strings.HasPrefix(name, "CSV") {
		return vm.execCSVCommand(name, arg)
	}

	if methodRes, handled, err := vm.execMethodLike(name, arg); handled {
		if err != nil {
			return execResult{}, err
		}
		if methodRes.Kind() == StringKind {
			vm.globals["RESULTS"] = methodRes
		} else {
			vm.globals["RESULT"] = methodRes
		}
		return execResult{kind: resultNone}, nil
	}

	if isAny(name, "CALL", "CALLF", "CALLFORM", "CALLFORMF", "TRYCALL", "TRYCALLFORM", "TRYCCALL", "TRYCCALLFORM", "CALLTRAIN", "JUMP", "JUMPFORM", "TRYJUMP", "TRYJUMPFORM", "TRYCJUMP", "TRYCJUMPFORM") {
		dynamic := strings.Contains(name, "FORM")
		target, args, err := vm.parseCommandCall(arg, dynamic)
		if err != nil {
			if strings.HasPrefix(name, "TRY") {
				return vm.handleTryFailure(name)
			}
			return execResult{}, err
		}
		if target == "" {
			if strings.HasPrefix(name, "TRY") {
				return vm.handleTryFailure(name)
			}
			return execResult{}, fmt.Errorf("%s without target", name)
		}
		if vm.program.Functions[target] == nil {
			if strings.HasPrefix(name, "TRY") {
				return vm.handleTryFailure(name)
			}
			return execResult{}, fmt.Errorf("function %s not found", target)
		}
		return vm.callFunction(target, args)
	}

	if isAny(name, "TRYCALLLIST", "TRYJUMPLIST", "TRYGOTOLIST", "CALLEVENT") {
		return vm.execCallListLike(name, arg)
	}

	return execResult{kind: resultNone}, nil
}

func (vm *VM) evalCommandPrint(name, arg string) (string, error) {
	if strings.Contains(name, "BUTTON") {
		return vm.evalPrintButton(arg)
	}
	if strings.HasPrefix(name, "PRINTS") || strings.HasPrefix(name, "DEBUGPRINTS") {
		return vm.evalPrintS(arg)
	}
	if strings.Contains(name, "FORMS") {
		return vm.evalPrintForms(arg)
	}
	if strings.Contains(name, "FORM") {
		return vm.evalPrintForm(arg)
	}
	if strings.HasPrefix(name, "PRINTV") || strings.HasPrefix(name, "DEBUGPRINTV") || strings.HasPrefix(name, "PRINTSINGLEV") {
		return vm.evalPrintV(arg)
	}
	return decodeCommandCharSeq(arg), nil
}

func (vm *VM) evalPrintS(arg string) (string, error) {
	v, err := vm.evalLooseExpr(arg)
	if err == nil {
		text := v.String()
		if v.Kind() == StringKind {
			if expanded, exErr := vm.expandDecodedTemplate(text); exErr == nil {
				return expanded, nil
			}
		}
		return text, nil
	}
	s := decodeCommandCharSeq(arg)
	if u, ok := tryUnquoteCommandString(s); ok {
		if ex, e := vm.expandFormTemplate(u); e == nil {
			return ex, nil
		}
		return u, nil
	}
	if ex, e := vm.expandDecodedTemplate(s); e == nil {
		return ex, nil
	}
	return s, nil
}

func (vm *VM) evalPrintButton(arg string) (string, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) == 0 {
		return "", nil
	}
	first := strings.TrimSpace(parts[0])
	if first == "" {
		return "", nil
	}
	v, err := vm.evalLooseExpr(first)
	if err == nil {
		if v.Kind() == StringKind {
			if s, e := vm.expandFormTemplate(v.String()); e == nil {
				return s, nil
			}
		}
		return v.String(), nil
	}
	s := decodeCommandCharSeq(first)
	if u, ok := tryUnquoteCommandString(s); ok {
		if ex, e := vm.expandFormTemplate(u); e == nil {
			return ex, nil
		}
		return u, nil
	}
	if ex, e := vm.expandFormTemplate(s); e == nil {
		return ex, nil
	}
	return s, nil
}

func (vm *VM) evalPrintForms(arg string) (string, error) {
	v, err := vm.evalCallArgRaw(arg)
	if err != nil {
		return "", err
	}
	return vm.evalPrintForm(v.String())
}

func (vm *VM) evalPrintV(arg string) (string, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) == 1 && strings.TrimSpace(parts[0]) == "" {
		return "", nil
	}
	var b strings.Builder
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "'") {
			b.WriteString(decodeCommandCharSeq(p[1:]))
			continue
		}
		v, err := vm.evalCallArgRaw(p)
		if err != nil {
			return "", err
		}
		b.WriteString(v.String())
	}
	return b.String(), nil
}

func (vm *VM) execSaveGame(arg string) (execResult, error) {
	slot := vm.evalSaveSlot(arg)
	if err := vm.saveGlobals(slot); err != nil {
		return execResult{}, err
	}
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execLoadGame(arg string) (execResult, error) {
	slot := vm.evalSaveSlot(arg)
	ok, err := vm.loadGlobals(slot)
	if err != nil {
		return execResult{}, err
	}
	if ok {
		vm.globals["RESULT"] = Int(1)
	} else {
		vm.globals["RESULT"] = Int(0)
	}
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execSaveData(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) == 1 {
		parts = strings.Fields(arg)
	}
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return execResult{}, fmt.Errorf("SAVEDATA needs slot")
	}
	slot, err := vm.evalSlotExpr(parts[0])
	if err != nil {
		return execResult{}, err
	}
	if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
		v, err := vm.evalLooseExpr(parts[1])
		if err != nil {
			return execResult{}, err
		}
		vm.globals["SAVEDATA_TEXT"] = Str(v.String())
	}
	if err := vm.saveGlobals(slot); err != nil {
		return execResult{}, err
	}
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execLoadData(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) == 1 {
		parts = strings.Fields(arg)
	}
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return execResult{}, fmt.Errorf("LOADDATA needs slot")
	}
	slot, err := vm.evalSlotExpr(parts[0])
	if err != nil {
		return execResult{}, err
	}
	ok, err := vm.loadGlobals(slot)
	if err != nil {
		return execResult{}, err
	}
	if ok {
		vm.globals["RESULT"] = Int(1)
	} else {
		vm.globals["RESULT"] = Int(0)
	}
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execDeleteData(arg string) (execResult, error) {
	slot, err := vm.evalSlotExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	if err := vm.deleteSave(slot); err != nil {
		return execResult{}, err
	}
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execCheckData(arg string) (execResult, error) {
	slot, err := vm.evalSlotExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	ok, err := vm.hasSave(slot)
	if err != nil {
		return execResult{}, err
	}
	if ok {
		vm.globals["RESULT"] = Int(1)
	} else {
		vm.globals["RESULT"] = Int(0)
	}
	return execResult{kind: resultNone}, nil
}

func (vm *VM) evalSaveSlot(arg string) string {
	slot, err := vm.evalSlotExpr(arg)
	if err != nil || slot == "" {
		return "default"
	}
	return slot
}

func (vm *VM) evalSlotExpr(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	v, err := vm.evalLooseExpr(raw)
	if err == nil {
		return sanitizeSlot(v.String()), nil
	}
	return sanitizeSlot(raw), nil
}

func sanitizeSlot(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	b := strings.Builder{}
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func (vm *VM) execCSVCommand(name, arg string) (execResult, error) {
	base := strings.TrimPrefix(strings.ToUpper(name), "CSV")
	args, err := vm.evalCommandArgs(arg)
	if err != nil {
		return execResult{}, err
	}
	if len(args) == 0 {
		vm.globals["RESULT"] = Int(0)
		return execResult{kind: resultNone}, nil
	}
	id := args[0].Int64()
	if val, ok := vm.csv.Name(base, id); ok {
		vm.globals["RESULT"] = Str(val)
	} else {
		vm.globals["RESULT"] = Str("")
	}
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execVarSet(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 2 {
		parts = strings.Fields(arg)
	}
	if len(parts) < 1 {
		return execResult{}, fmt.Errorf("VARSET/CVARSET requires variable")
	}
	target, err := vm.parseVarRefRuntime(parts[0])
	if err != nil {
		return execResult{}, fmt.Errorf("VARSET/CVARSET invalid variable: %w", err)
	}
	if len(parts) == 1 {
		if err := vm.resetVarSetTarget(target); err != nil {
			return execResult{}, err
		}
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	}

	val, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return execResult{}, err
	}

	if len(parts) >= 3 {
		startV, err := vm.evalLooseExpr(parts[2])
		if err != nil {
			return execResult{}, err
		}
		end := startV.Int64()
		if len(parts) >= 4 {
			endV, err := vm.evalLooseExpr(parts[3])
			if err != nil {
				return execResult{}, err
			}
			end = endV.Int64()
		}
		if err := vm.varSetRange(target, val, startV.Int64(), end); err != nil {
			return execResult{}, err
		}
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	}

	if err := vm.setVarRef(target, val); err != nil {
		return execResult{}, err
	}
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) resetVarSetTarget(target ast.VarRef) error {
	name := strings.ToUpper(strings.TrimSpace(target.Name))
	if name == "" {
		return nil
	}
	if len(target.Index) == 0 {
		if arr, ok := vm.lookupArray(name); ok {
			arr.Data = map[string]Value{}
			if isResultLikeName(name) {
				vm.globals[name] = arr.defaultValue()
			}
			return nil
		}
		def, err := vm.defaultValueForVarRef(target)
		if err != nil {
			return err
		}
		return vm.setVarRef(target, def)
	}

	prefix, err := vm.evalIndexExprsFor(target.Name, target.Index)
	if err != nil {
		return err
	}
	if arr, ok := vm.lookupArray(name); ok {
		vm.clearArrayByPrefix(arr, prefix)
		return nil
	}
	def, err := vm.defaultValueForVarRef(target)
	if err != nil {
		return err
	}
	return vm.setVarRef(target, def)
}

func (vm *VM) varSetRange(target ast.VarRef, val Value, start, end int64) error {
	name := strings.ToUpper(strings.TrimSpace(target.Name))
	prefix, err := vm.evalIndexExprsFor(target.Name, target.Index)
	if err != nil {
		return err
	}
	if end < start {
		start, end = end, start
	}
	arr, ok := vm.lookupArray(name)
	if !ok {
		dims := make([]int, len(prefix)+1)
		for i, v := range prefix {
			d := int(v) + 1
			if d < 1 {
				d = 1
			}
			dims[i] = d
		}
		d := int(end) + 1
		if d < 1 {
			d = 1
		}
		dims[len(prefix)] = d
		arr = newArrayVar(val.Kind() == StringKind, true, dims)
		if fr := vm.currentFrame(); fr != nil && strings.HasPrefix(name, "LOCAL") {
			fr.lArrays[name] = arr
		} else {
			vm.gArrays[name] = arr
		}
	}
	if val.Kind() == StringKind {
		arr.IsString = true
	}
	for i := start; i <= end; i++ {
		idx := make([]int64, 0, len(prefix)+1)
		idx = append(idx, prefix...)
		idx = append(idx, i)
		if err := arr.Set(idx, val); err != nil {
			return err
		}
	}
	if isResultLikeName(name) && len(prefix) == 0 && start <= 0 && 0 <= end {
		vm.globals[name] = val
	}
	return nil
}

func (vm *VM) clearArrayByPrefix(arr *ArrayVar, prefix []int64) {
	if arr == nil {
		return
	}
	if len(prefix) == 0 {
		arr.Data = map[string]Value{}
		return
	}
	for k := range arr.Data {
		if !arrayKeyHasPrefix(k, prefix) {
			continue
		}
		delete(arr.Data, k)
	}
}

func arrayKeyHasPrefix(key string, prefix []int64) bool {
	parts := strings.Split(key, ":")
	if len(parts) < len(prefix) {
		return false
	}
	for i, want := range prefix {
		got, err := strconv.ParseInt(parts[i], 10, 64)
		if err != nil || got != want {
			return false
		}
	}
	return true
}

func (vm *VM) execBitMutation(name, arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 2 {
		parts = strings.Fields(arg)
	}
	if len(parts) < 2 {
		return execResult{}, fmt.Errorf("%s requires variable and bit", name)
	}
	target, err := vm.parseVarRefRuntime(parts[0])
	if err != nil {
		return execResult{}, fmt.Errorf("%s invalid variable: %w", name, err)
	}
	bitVal, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return execResult{}, err
	}
	bit := uint(bitVal.Int64())
	curVal, err := vm.getVarRef(target)
	if err != nil {
		return execResult{}, err
	}
	cur := curVal.Int64()
	mask := int64(1) << bit
	switch name {
	case "SETBIT":
		cur = cur | mask
	case "CLEARBIT":
		cur = cur &^ mask
	case "INVERTBIT":
		cur = cur ^ mask
	}
	if err := vm.setVarRef(target, Int(cur)); err != nil {
		return execResult{}, err
	}
	vm.globals["RESULT"] = Int(cur)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execGetBit(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 2 {
		parts = strings.Fields(arg)
	}
	if len(parts) < 2 {
		return execResult{}, fmt.Errorf("GETBIT requires value and bit")
	}
	v, err := vm.evalLooseExpr(parts[0])
	if err != nil {
		return execResult{}, err
	}
	bitVal, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return execResult{}, err
	}
	bit := uint(bitVal.Int64())
	mask := int64(1) << bit
	if (v.Int64() & mask) != 0 {
		vm.globals["RESULT"] = Int(1)
	} else {
		vm.globals["RESULT"] = Int(0)
	}
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execTimes(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 2 {
		parts = strings.Fields(arg)
	}
	if len(parts) < 2 {
		return execResult{}, fmt.Errorf("TIMES requires variable and factor")
	}
	target, err := vm.parseVarRefRuntime(parts[0])
	if err != nil {
		return execResult{}, err
	}
	base, err := vm.getVarRef(target)
	if err != nil {
		return execResult{}, err
	}
	factorRaw := strings.TrimSpace(parts[1])
	factor := 1.0
	if v, err := vm.evalLooseExpr(factorRaw); err == nil {
		if fv, ferr := strconv.ParseFloat(v.String(), 64); ferr == nil {
			factor = fv
		}
	}
	res := int64(float64(base.Int64()) * factor)
	if err := vm.setVarRef(target, Int(res)); err != nil {
		return execResult{}, err
	}
	vm.globals["RESULT"] = Int(res)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execSplit(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 3 {
		return execResult{}, fmt.Errorf("SPLIT requires value, separator, destination")
	}
	valueV, err := vm.evalLooseExpr(parts[0])
	if err != nil {
		return execResult{}, err
	}
	sepV, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return execResult{}, err
	}
	dest, err := vm.parseVarRefRuntime(parts[2])
	if err != nil {
		return execResult{}, err
	}
	chunks := strings.Split(valueV.String(), sepV.String())
	baseIdx, err := vm.evalIndexExprsFor(dest.Name, dest.Index)
	if err != nil {
		return execResult{}, err
	}
	for i, c := range chunks {
		idx := append([]int64{}, baseIdx...)
		idx = append(idx, int64(i))
		// direct set via array lookup to avoid rebuilding expression indices
		arr, ok := vm.lookupArray(strings.ToUpper(dest.Name))
		if !ok {
			dims := make([]int, len(idx))
			for di, iv := range idx {
				dims[di] = int(iv) + len(chunks)
				if dims[di] < 1 {
					dims[di] = 1
				}
			}
			arr = newArrayVar(true, true, dims)
			vm.gArrays[strings.ToUpper(dest.Name)] = arr
		}
		_ = arr.Set(idx, Str(c))
	}
	vm.globals["RESULT"] = Int(int64(len(chunks)))
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execEscape(arg string) (execResult, error) {
	v, err := vm.evalLooseExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	s := v.String()
	repl := []struct{ old, new string }{
		{"\\", "\\\\"}, {"*", "\\*"}, {"+", "\\+"}, {"?", "\\?"},
		{"|", "\\|"}, {"{", "\\{"}, {"}", "\\}"}, {"[", "\\["},
		{"]", "\\]"}, {"(", "\\("}, {")", "\\)"}, {"^", "\\^"},
		{"$", "\\$"}, {".", "\\."}, {"#", "\\#"},
	}
	for _, r := range repl {
		s = strings.ReplaceAll(s, r.old, r.new)
	}
	vm.globals["RESULTS"] = Str(s)
	vm.globals["RESULT"] = Int(int64(len(s)))
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execEncodeToUni(arg string) (execResult, error) {
	v, err := vm.evalLooseExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	buf := []byte(v.String())
	vm.globals["RESULT"] = Int(int64(len(buf)))
	for i, b := range buf {
		vm.globals[fmt.Sprintf("RESULT%d", i+1)] = Int(int64(b))
	}
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execPutForm(arg string) (execResult, error) {
	text, err := vm.evalPrintForm(arg)
	if err != nil {
		return execResult{}, err
	}
	prev := ""
	if v, ok := vm.globals["SAVEDATA_TEXT"]; ok {
		prev = v.String()
	}
	vm.setVar("SAVEDATA_TEXT", Str(prev+text))
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execBar(name, arg string) (execResult, error) {
	args, err := vm.evalCommandArgs(arg)
	if err != nil {
		return execResult{}, err
	}
	if len(args) < 3 {
		return execResult{}, fmt.Errorf("%s requires value,max,length", name)
	}
	val, maxV, length := args[0].Int64(), args[1].Int64(), args[2].Int64()
	if maxV <= 0 {
		maxV = 1
	}
	if length < 0 {
		length = 0
	}
	filled := length * val / maxV
	if filled < 0 {
		filled = 0
	}
	if filled > length {
		filled = length
	}
	text := "[" + strings.Repeat("*", int(filled)) + strings.Repeat(".", int(length-filled)) + "]"
	if !vm.ui.SkipDisp {
		vm.emitOutput(Output{Text: text, NewLine: name == "BARL"})
	}
	vm.globals["RESULTS"] = Str(text)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execResetGlobal() (execResult, error) {
	vm.globals = map[string]Value{}
	vm.gArrays = map[string]*ArrayVar{}
	vm.gRefs = map[string]ast.VarRef{}
	vm.gRefDecl = map[string]bool{}
	if err := vm.initDefines(); err != nil {
		return execResult{}, err
	}
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execResetData() (execResult, error) {
	res, err := vm.execResetGlobal()
	if err != nil {
		return execResult{}, err
	}
	vm.characters = nil
	vm.nextCharID = 0
	vm.refreshCharacterGlobals()
	return res, nil
}

func (vm *VM) execSwap(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 2 {
		parts = strings.Fields(arg)
	}
	if len(parts) < 2 {
		return execResult{}, fmt.Errorf("SWAP requires two variables")
	}
	a, err := vm.parseVarRefRuntime(parts[0])
	if err != nil {
		return execResult{}, err
	}
	b, err := vm.parseVarRefRuntime(parts[1])
	if err != nil {
		return execResult{}, err
	}
	av, err := vm.getVarRef(a)
	if err != nil {
		return execResult{}, err
	}
	bv, err := vm.getVarRef(b)
	if err != nil {
		return execResult{}, err
	}
	if err := vm.setVarRef(a, bv); err != nil {
		return execResult{}, err
	}
	if err := vm.setVarRef(b, av); err != nil {
		return execResult{}, err
	}
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execArrayShift(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 2 {
		parts = strings.Fields(arg)
	}
	if len(parts) < 2 {
		return execResult{}, fmt.Errorf("ARRAYSHIFT requires variable and index")
	}
	ref, err := vm.parseVarRefRuntime(parts[0])
	if err != nil {
		return execResult{}, err
	}
	if len(ref.Index) != 0 {
		return execResult{}, fmt.Errorf("ARRAYSHIFT expects base array variable")
	}
	startV, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return execResult{}, err
	}
	count := int64(1)
	if len(parts) >= 3 {
		cv, err := vm.evalLooseExpr(parts[2])
		if err == nil && cv.Int64() > 0 {
			count = cv.Int64()
		}
	}
	arr, ok := vm.lookupArray(strings.ToUpper(ref.Name))
	if !ok {
		return execResult{}, fmt.Errorf("ARRAYSHIFT target is not an array")
	}
	if len(arr.Dims) == 0 {
		vm.globals["RESULT"] = Int(0)
		return execResult{kind: resultNone}, nil
	}
	n := int64(arr.Dims[0])
	start := startV.Int64()
	if start < 0 {
		start = 0
	}
	if start >= n {
		vm.globals["RESULT"] = Int(0)
		return execResult{kind: resultNone}, nil
	}
	if count < 1 {
		count = 1
	}
	for i := start; i < n; i++ {
		src := i + count
		var val Value
		if src >= 0 && src < n {
			val, _ = arr.Get([]int64{src})
		} else {
			val = arr.defaultValue()
		}
		_ = arr.Set([]int64{i}, val)
	}
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execArrayRemove(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 2 {
		parts = strings.Fields(arg)
	}
	if len(parts) < 2 {
		return execResult{}, fmt.Errorf("ARRAYREMOVE requires variable and index")
	}
	ref, err := vm.parseVarRefRuntime(parts[0])
	if err != nil {
		return execResult{}, err
	}
	if len(ref.Index) != 0 {
		return execResult{}, fmt.Errorf("ARRAYREMOVE expects base array variable")
	}
	idxV, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return execResult{}, err
	}
	arr, ok := vm.lookupArray(strings.ToUpper(ref.Name))
	if !ok {
		return execResult{}, fmt.Errorf("ARRAYREMOVE target is not an array")
	}
	_ = arr.Set([]int64{idxV.Int64()}, arr.defaultValue())
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execArrayCopy(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 2 {
		parts = strings.Fields(arg)
	}
	if len(parts) < 2 {
		return execResult{}, fmt.Errorf("ARRAYCOPY requires destination and source")
	}
	dstRef, err := vm.parseVarRefRuntime(parts[0])
	if err != nil {
		return execResult{}, err
	}
	srcRef, err := vm.parseVarRefRuntime(parts[1])
	if err != nil {
		return execResult{}, err
	}
	if len(dstRef.Index) != 0 || len(srcRef.Index) != 0 {
		return execResult{}, fmt.Errorf("ARRAYCOPY expects base array variables")
	}
	src, ok := vm.lookupArray(strings.ToUpper(srcRef.Name))
	if !ok {
		return execResult{}, fmt.Errorf("ARRAYCOPY source is not an array")
	}
	dstName := strings.ToUpper(dstRef.Name)
	dst, ok := vm.lookupArray(dstName)
	if !ok {
		dst = newArrayVar(src.IsString, src.IsDynamic, src.Dims)
		if fr := vm.currentFrame(); fr != nil {
			if _, exists := fr.lArrays[dstName]; exists {
				fr.lArrays[dstName] = dst
			} else {
				vm.gArrays[dstName] = dst
			}
		} else {
			vm.gArrays[dstName] = dst
		}
	}
	dst.IsString = src.IsString
	dst.IsDynamic = src.IsDynamic
	dst.Dims = append(dst.Dims[:0], src.Dims...)
	dst.Data = map[string]Value{}
	for k, v := range src.Data {
		dst.Data[k] = v
	}
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execArraySort(arg string) (execResult, error) {
	ref, err := vm.parseVarRefRuntime(arg)
	if err != nil {
		return execResult{}, err
	}
	if len(ref.Index) != 0 {
		return execResult{}, fmt.Errorf("ARRAYSORT expects base array variable")
	}
	arr, ok := vm.lookupArray(strings.ToUpper(ref.Name))
	if !ok {
		return execResult{}, fmt.Errorf("ARRAYSORT target is not an array")
	}
	if len(arr.Dims) == 0 {
		vm.globals["RESULT"] = Int(0)
		return execResult{kind: resultNone}, nil
	}
	n := arr.Dims[0]
	if n <= 1 {
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	}
	vals := make([]Value, n)
	for i := 0; i < n; i++ {
		v, _ := arr.Get([]int64{int64(i)})
		vals[i] = v
	}
	sort.Slice(vals, func(i, j int) bool {
		if arr.IsString {
			return vals[i].String() < vals[j].String()
		}
		return vals[i].Int64() < vals[j].Int64()
	})
	for i := 0; i < n; i++ {
		_ = arr.Set([]int64{int64(i)}, vals[i])
	}
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execDrawLine(name, arg string) (execResult, error) {
	text := strings.Repeat("-", 40)
	if strings.Contains(name, "FORM") {
		v, err := vm.evalPrintForm(arg)
		if err == nil && strings.TrimSpace(v) != "" {
			text = v
		}
	} else if strings.TrimSpace(arg) != "" {
		v, err := vm.evalLooseExpr(arg)
		if err == nil && strings.TrimSpace(v.String()) != "" {
			text = v.String()
		}
	}
	vm.emitOutput(Output{Text: text, NewLine: true})
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execClearLine(arg string) (execResult, error) {
	n := int64(1)
	if strings.TrimSpace(arg) != "" {
		v, err := vm.evalLooseExpr(arg)
		if err == nil && v.Int64() > 0 {
			n = v.Int64()
		}
	}
	if n > int64(len(vm.outputs)) {
		n = int64(len(vm.outputs))
	}
	vm.outputs = vm.outputs[:len(vm.outputs)-int(n)]
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execReuseLastLine() (execResult, error) {
	if len(vm.outputs) == 0 {
		return execResult{kind: resultNone}, nil
	}
	last := vm.outputs[len(vm.outputs)-1]
	vm.emitOutput(last)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execAlignment(arg string) (execResult, error) {
	if strings.TrimSpace(arg) == "" {
		vm.globals["RESULT"] = Str(vm.ui.Align)
		return execResult{kind: resultNone}, nil
	}
	v, err := vm.evalLooseExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	vm.ui.Align = normalizeAlign(v.String())
	vm.globals["RESULT"] = Str(vm.ui.Align)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execRedraw(arg string) (execResult, error) {
	if strings.TrimSpace(arg) == "" {
		vm.ui.Redraw = true
	} else {
		v, err := vm.evalLooseExpr(arg)
		if err != nil {
			return execResult{}, err
		}
		vm.ui.Redraw = v.Int64() != 0
	}
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execSkipDisp(arg string) (execResult, error) {
	if strings.TrimSpace(arg) == "" {
		vm.ui.SkipDisp = true
	} else {
		v, err := vm.evalLooseExpr(arg)
		if err != nil {
			return execResult{}, err
		}
		vm.ui.SkipDisp = v.Int64() != 0
	}
	if vm.ui.SkipDisp {
		vm.globals["RESULT"] = Int(1)
	} else {
		vm.globals["RESULT"] = Int(0)
	}
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execSetColor(arg string) (execResult, error) {
	v, err := vm.evalLooseExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	vm.ui.Color = strings.TrimSpace(v.String())
	vm.globals["RESULT"] = Str(vm.ui.Color)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execSetBgColor(arg string) (execResult, error) {
	v, err := vm.evalLooseExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	vm.ui.BgColor = strings.TrimSpace(v.String())
	vm.globals["RESULT"] = Str(vm.ui.BgColor)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execFontStyle(arg string) (execResult, error) {
	v, err := vm.evalLooseExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	style := v.Int64()
	vm.ui.Bold = (style & 1) != 0
	vm.ui.Italic = (style & 2) != 0
	vm.globals["RESULT"] = Int(style)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execPrintCPerLine(arg string) (execResult, error) {
	v, err := vm.evalLooseExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	n := v.Int64()
	if n <= 0 {
		n = 1
	}
	vm.ui.PrintCPL = n
	vm.globals["RESULT"] = Int(n)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execAddChara(arg string) (execResult, error) {
	id := int64(-1)
	if strings.TrimSpace(arg) != "" {
		v, err := vm.evalLooseExpr(arg)
		if err == nil {
			id = v.Int64()
		}
	}
	idx := vm.addCharacter(id)
	vm.globals["RESULT"] = Int(idx)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execDelChara(arg string) (execResult, error) {
	v, err := vm.evalLooseExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	if vm.deleteCharacterAt(v.Int64()) {
		vm.globals["RESULT"] = Int(1)
	} else {
		vm.globals["RESULT"] = Int(0)
	}
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execGetChara(arg string) (execResult, error) {
	if strings.TrimSpace(arg) == "" {
		vm.globals["RESULT"] = Int(int64(len(vm.characters)))
		return execResult{kind: resultNone}, nil
	}
	v, err := vm.evalLooseExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	i := v.Int64()
	if i < 0 || i >= int64(len(vm.characters)) {
		vm.globals["RESULT"] = Int(-1)
	} else {
		vm.globals["RESULT"] = Int(vm.characters[i].ID)
	}
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execFindChara(arg string, reverse bool) (execResult, error) {
	v, err := vm.evalLooseExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	id := v.Int64()
	if reverse {
		for i := len(vm.characters) - 1; i >= 0; i-- {
			if vm.characters[i].ID == id {
				vm.globals["RESULT"] = Int(int64(i))
				return execResult{kind: resultNone}, nil
			}
		}
	} else {
		for i := 0; i < len(vm.characters); i++ {
			if vm.characters[i].ID == id {
				vm.globals["RESULT"] = Int(int64(i))
				return execResult{kind: resultNone}, nil
			}
		}
	}
	vm.globals["RESULT"] = Int(-1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execSwapChara(arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 2 {
		parts = strings.Fields(arg)
	}
	if len(parts) < 2 {
		return execResult{}, fmt.Errorf("SWAPCHARA requires 2 indices")
	}
	a, err := vm.evalLooseExpr(parts[0])
	if err != nil {
		return execResult{}, err
	}
	b, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return execResult{}, err
	}
	ai, bi := int(a.Int64()), int(b.Int64())
	if ai < 0 || bi < 0 || ai >= len(vm.characters) || bi >= len(vm.characters) {
		vm.globals["RESULT"] = Int(0)
		return execResult{kind: resultNone}, nil
	}
	vm.characters[ai], vm.characters[bi] = vm.characters[bi], vm.characters[ai]
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execCopyChara(arg string, add bool) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 1 {
		parts = strings.Fields(arg)
	}
	if len(parts) < 1 {
		return execResult{}, fmt.Errorf("COPYCHARA requires source index")
	}
	srcV, err := vm.evalLooseExpr(parts[0])
	if err != nil {
		return execResult{}, err
	}
	src := int(srcV.Int64())
	if src < 0 || src >= len(vm.characters) {
		vm.globals["RESULT"] = Int(0)
		return execResult{kind: resultNone}, nil
	}
	if add || len(parts) < 2 {
		idx := vm.addCharacter(vm.characters[src].ID)
		vm.globals["RESULT"] = Int(idx)
		return execResult{kind: resultNone}, nil
	}
	dstV, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return execResult{}, err
	}
	dst := int(dstV.Int64())
	if dst < 0 || dst >= len(vm.characters) {
		vm.globals["RESULT"] = Int(0)
		return execResult{kind: resultNone}, nil
	}
	vm.characters[dst].ID = vm.characters[src].ID
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execPickupChara(arg string) (execResult, error) {
	v, err := vm.evalLooseExpr(arg)
	if err != nil {
		return execResult{}, err
	}
	id := v.Int64()
	for i := range vm.characters {
		if vm.characters[i].ID == id {
			vm.globals["RESULT"] = Int(int64(i))
			return execResult{kind: resultNone}, nil
		}
	}
	vm.globals["RESULT"] = Int(-1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) lookupArray(name string) (*ArrayVar, bool) {
	name = strings.ToUpper(name)
	if fr := vm.currentFrame(); fr != nil {
		if arr, ok := fr.lArrays[name]; ok {
			return arr, true
		}
	}
	arr, ok := vm.gArrays[name]
	return arr, ok
}

func (vm *VM) execRefBinding(name, arg string) (execResult, error) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 2 {
		parts = strings.Fields(arg)
	}
	if len(parts) < 2 {
		return execResult{}, fmt.Errorf("%s requires destination and source", name)
	}
	dst, err := vm.parseVarRefRuntime(parts[0])
	if err != nil {
		return execResult{}, err
	}
	if len(dst.Index) != 0 {
		return execResult{}, fmt.Errorf("%s destination must be a base variable", name)
	}
	var src ast.VarRef
	if name == "REFBYNAME" {
		v, err := vm.evalLooseExpr(parts[1])
		if err != nil {
			return execResult{}, err
		}
		src, err = vm.parseVarRefRuntime(v.String())
		if err != nil {
			return execResult{}, err
		}
	} else {
		src, err = vm.parseVarRefRuntime(parts[1])
		if err != nil {
			return execResult{}, err
		}
	}
	if !vm.isRefDeclared(dst.Name) {
		vm.globals["RESULT"] = Int(0)
		return execResult{kind: resultNone}, nil
	}
	vm.setRefBinding(strings.ToUpper(dst.Name), src)
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
}

func (vm *VM) execMethodLike(name, arg string) (Value, bool, error) {
	args, err := vm.evalCommandArgs(arg)
	if err != nil {
		return Value{}, false, err
	}
	switch name {
	case "HTMLP":
		if len(args) < 1 {
			return Str(""), true, nil
		}
		align := strings.TrimSpace(strings.ToLower(vm.ui.Align))
		if align == "" {
			align = "left"
		}
		if len(args) >= 2 {
			a := strings.TrimSpace(strings.ToLower(args[1].String()))
			switch a {
			case "", "left", "center", "right":
				if a != "" {
					align = a
				}
			case "왼쪽":
				align = "left"
			case "중앙":
				align = "center"
			case "오른쪽":
				align = "right"
			}
		}
		return Str("<p align='" + align + "'>" + args[0].String() + "</p>"), true, nil
	case "HTMLFONT":
		if len(args) < 1 {
			return Str(""), true, nil
		}
		text := args[0].String()
		face := ""
		color := ""
		bcolor := ""
		if len(args) >= 2 {
			face = strings.TrimSpace(args[1].String())
		}
		if len(args) >= 3 {
			if c := vm.htmlColorFromValue(args[2]); c != "" {
				color = c
			}
		}
		if len(args) >= 4 {
			if c := vm.htmlColorFromValue(args[3]); c != "" {
				bcolor = c
			}
		}
		if face == "" && color == "" && bcolor == "" {
			face = strings.TrimSpace(vm.ui.Font)
			if c := vm.htmlColorFromValue(Int(-1)); c != "" {
				color = c
			}
		}
		var b strings.Builder
		b.WriteString("<font")
		if face != "" {
			b.WriteString(" face='")
			b.WriteString(face)
			b.WriteString("'")
		}
		if color != "" {
			b.WriteString(" color='")
			b.WriteString(color)
			b.WriteString("'")
		}
		if bcolor != "" {
			b.WriteString(" bcolor='")
			b.WriteString(bcolor)
			b.WriteString("'")
		}
		b.WriteString(">")
		b.WriteString(text)
		b.WriteString("</font>")
		return Str(b.String()), true, nil
	case "HTMLNOBR":
		if len(args) < 1 {
			return Str("<nobr></nobr>"), true, nil
		}
		return Str("<nobr>" + args[0].String() + "</nobr>"), true, nil
	case "HTMLSTYLE":
		if len(args) < 1 {
			return Str(""), true, nil
		}
		style := int64(0)
		if vm.ui.Bold {
			style |= 1
		}
		if vm.ui.Italic {
			style |= 2
		}
		if len(args) >= 2 {
			s := args[1].Int64()
			if s >= 0 {
				style = s
			}
		}
		text := args[0].String()
		open := ""
		close := ""
		if style&1 != 0 {
			open += "<b>"
			close = "</b>" + close
		}
		if style&2 != 0 {
			open += "<i>"
			close = "</i>" + close
		}
		if style&4 != 0 {
			open += "<s>"
			close = "</s>" + close
		}
		if style&8 != 0 {
			open += "<u>"
			close = "</u>" + close
		}
		return Str(open + text + close), true, nil
	case "HTMLCOLOR":
		if len(args) < 1 {
			if c := vm.htmlColorFromValue(Int(-1)); c != "" {
				return Str(c), true, nil
			}
			return Str("FFFFFF"), true, nil
		}
		if c := vm.htmlColorFromValue(args[0]); c != "" {
			return Str(c), true, nil
		}
		return Str("FFFFFF"), true, nil
	case "HTMLBUTTON", "HTMLAUTOBUTTON", "HTMLNONBUTTON":
		if len(args) < 1 {
			return Str(""), true, nil
		}
		return Str(args[0].String()), true, nil
	case "SUMARRAY", "SUMCARRAY":
		return vm.execMethodSumArray(arg), true, nil
	case "MATCH", "CMATCH":
		return vm.execMethodMatch(arg), true, nil
	case "GROUPMATCH":
		return vm.execMethodGroupMatch(args), true, nil
	case "NOSAMES":
		return vm.execMethodNoSames(args), true, nil
	case "ALLSAMES":
		return vm.execMethodAllSames(args), true, nil
	case "MAXARRAY", "MAXCARRAY":
		return vm.execMethodMaxMinArray(arg, true), true, nil
	case "MINARRAY", "MINCARRAY":
		return vm.execMethodMaxMinArray(arg, false), true, nil
	case "GETNUM":
		return vm.execMethodGetNum(arg, false), true, nil
	case "GETNUMB":
		return vm.execMethodGetNum(arg, true), true, nil
	case "FINDELEMENT":
		return vm.execMethodFindElement(arg, false), true, nil
	case "FINDLASTELEMENT":
		return vm.execMethodFindElement(arg, true), true, nil
	case "INRANGEARRAY", "INRANGECARRAY":
		return vm.execMethodInRangeArray(arg), true, nil
	case "VARSIZE":
		n, err := vm.evalVarSizeRaw(arg)
		if err != nil {
			return Value{}, true, err
		}
		return Int(n), true, nil
	case "UNICODE":
		if len(args) < 1 {
			return Str(""), true, nil
		}
		return Str(string(rune(args[0].Int64()))), true, nil
	case "ENCODETOUNI":
		if len(args) < 1 {
			return Int(0), true, nil
		}
		return Int(int64(len([]byte(args[0].String())))), true, nil
	case "ABS":
		if len(args) < 1 {
			return Int(0), true, nil
		}
		v := args[0].Int64()
		if v < 0 {
			v = -v
		}
		return Int(v), true, nil
	case "SIGN":
		if len(args) < 1 {
			return Int(0), true, nil
		}
		v := args[0].Int64()
		if v < 0 {
			return Int(-1), true, nil
		}
		if v > 0 {
			return Int(1), true, nil
		}
		return Int(0), true, nil
	case "MAX":
		if len(args) == 0 {
			return Int(0), true, nil
		}
		m := args[0].Int64()
		for _, a := range args[1:] {
			if a.Int64() > m {
				m = a.Int64()
			}
		}
		return Int(m), true, nil
	case "MIN":
		if len(args) == 0 {
			return Int(0), true, nil
		}
		m := args[0].Int64()
		for _, a := range args[1:] {
			if a.Int64() < m {
				m = a.Int64()
			}
		}
		return Int(m), true, nil
	case "POWER":
		if len(args) < 2 {
			return Int(0), true, nil
		}
		base := args[0].Int64()
		exp := args[1].Int64()
		if exp < 0 {
			return Int(0), true, nil
		}
		acc := int64(1)
		for i := int64(0); i < exp; i++ {
			acc *= base
		}
		return Int(acc), true, nil
	case "BARSTR":
		if len(args) < 3 {
			return Str(""), true, nil
		}
		val, maxV, length := args[0].Int64(), args[1].Int64(), args[2].Int64()
		if maxV <= 0 {
			maxV = 1
		}
		if length < 0 {
			length = 0
		}
		filled := length * val / maxV
		if filled < 0 {
			filled = 0
		}
		if filled > length {
			filled = length
		}
		return Str("[" + strings.Repeat("*", int(filled)) + strings.Repeat(".", int(length-filled)) + "]"), true, nil
	case "SQRT":
		if len(args) < 1 {
			return Int(0), true, nil
		}
		v := args[0].Int64()
		if v < 0 {
			return Int(0), true, nil
		}
		return Int(int64(math.Sqrt(float64(v)))), true, nil
	case "LIMIT":
		if len(args) < 3 {
			return Int(0), true, nil
		}
		v := args[0].Int64()
		minV := args[1].Int64()
		maxV := args[2].Int64()
		if v < minV {
			v = minV
		}
		if v > maxV {
			v = maxV
		}
		return Int(v), true, nil
	case "INRANGE":
		if len(args) < 3 {
			return Int(0), true, nil
		}
		v, lo, hi := args[0].Int64(), args[1].Int64(), args[2].Int64()
		if v >= lo && v <= hi {
			return Int(1), true, nil
		}
		return Int(0), true, nil
	case "RAND":
		if len(args) < 1 {
			return Int(0), true, nil
		}
		n := args[0].Int64()
		if n <= 0 {
			return Int(0), true, nil
		}
		return Int(vm.rng.Int63n(n)), true, nil
	case "STRLEN", "STRLENU", "STRLENS", "STRLENSU":
		if len(args) < 1 {
			return Int(0), true, nil
		}
		return Int(int64(len([]rune(args[0].String())))), true, nil
	case "STRLENFORM", "STRLENFORMU":
		text, err := vm.evalPrintForm(arg)
		if err != nil {
			return Value{}, true, err
		}
		return Int(int64(len([]rune(text)))), true, nil
	case "STRFIND", "STRFINDU":
		if len(args) < 2 {
			return Int(-1), true, nil
		}
		return Int(int64(strings.Index(args[0].String(), args[1].String()))), true, nil
	case "LINEISEMPTY":
		if len(vm.outputs) == 0 {
			return Int(1), true, nil
		}
		last := vm.outputs[len(vm.outputs)-1]
		if strings.TrimSpace(last.Text) == "" {
			return Int(1), true, nil
		}
		return Int(0), true, nil
	case "GETSTYLE":
		style := int64(0)
		if vm.ui.Bold {
			style |= 1
		}
		if vm.ui.Italic {
			style |= 2
		}
		return Int(style), true, nil
	case "SUBSTRING", "SUBSTRINGU":
		if len(args) < 2 {
			return Str(""), true, nil
		}
		src := []rune(args[0].String())
		start := args[1].Int64()
		if start < 0 {
			start = 0
		}
		if start > int64(len(src)) {
			start = int64(len(src))
		}
		end := int64(len(src))
		if len(args) >= 3 {
			n := args[2].Int64()
			if n < 0 {
				n = 0
			}
			end = start + n
			if end > int64(len(src)) {
				end = int64(len(src))
			}
		}
		return Str(string(src[start:end])), true, nil
	case "TOINT":
		if len(args) < 1 {
			return Int(0), true, nil
		}
		return Int(args[0].Int64()), true, nil
	case "TOSTR":
		if len(args) < 1 {
			return Str(""), true, nil
		}
		return Str(args[0].String()), true, nil
	case "TOUPPER":
		if len(args) < 1 {
			return Str(""), true, nil
		}
		return Str(strings.ToUpper(args[0].String())), true, nil
	case "TOLOWER":
		if len(args) < 1 {
			return Str(""), true, nil
		}
		return Str(strings.ToLower(args[0].String())), true, nil
	case "TOHALF":
		if len(args) < 1 {
			return Str(""), true, nil
		}
		return Str(toHalfWidth(args[0].String())), true, nil
	case "TOFULL":
		if len(args) < 1 {
			return Str(""), true, nil
		}
		return Str(toFullWidth(args[0].String())), true, nil
	case "REPLACE":
		if len(args) < 3 {
			return Str(""), true, nil
		}
		return Str(strings.ReplaceAll(args[0].String(), args[1].String(), args[2].String())), true, nil
	case "STRCOUNT":
		if len(args) < 2 {
			return Int(0), true, nil
		}
		return Int(int64(strings.Count(args[0].String(), args[1].String()))), true, nil
	case "STRJOIN":
		if len(args) < 2 {
			if len(args) == 1 {
				return Str(args[0].String()), true, nil
			}
			return Str(""), true, nil
		}
		sep := args[0].String()
		parts := make([]string, 0, len(args)-1)
		for _, a := range args[1:] {
			parts = append(parts, a.String())
		}
		return Str(strings.Join(parts, sep)), true, nil
	case "STRFORM":
		if len(args) < 1 {
			return Str(""), true, nil
		}
		out := args[0].String()
		out, err = vm.evalPercentPlaceholders(out)
		if err != nil {
			return Value{}, true, err
		}
		out, err = vm.evalBracePlaceholders(out)
		if err != nil {
			return Value{}, true, err
		}
		return Str(out), true, nil
	case "CHARATU":
		if len(args) < 2 {
			return Str(""), true, nil
		}
		r := []rune(args[0].String())
		idx := args[1].Int64()
		if idx < 0 || idx >= int64(len(r)) {
			return Str(""), true, nil
		}
		return Str(string(r[idx])), true, nil
	case "CONVERT":
		if len(args) < 2 {
			return Str(""), true, nil
		}
		base := args[1].Int64()
		if base != 2 && base != 8 && base != 10 && base != 16 {
			return Value{}, true, fmt.Errorf("CONVERT base must be one of 2,8,10,16")
		}
		return Str(strconv.FormatInt(args[0].Int64(), int(base))), true, nil
	case "ISNUMERIC":
		if len(args) < 1 {
			return Int(0), true, nil
		}
		if isNumericLike(args[0].String()) {
			return Int(1), true, nil
		}
		return Int(0), true, nil
	case "GETTIMES":
		return Str(time.Now().Format("2006/01/02 15:04:05")), true, nil
	case "MONEYSTR":
		if len(args) < 1 {
			return Str("0"), true, nil
		}
		return Str(strconv.FormatInt(args[0].Int64(), 10)), true, nil
	case "EXISTCSV":
		if len(args) < 1 {
			return Int(0), true, nil
		}
		if args[0].Kind() != StringKind {
			if vm.csv.ExistsID(args[0].Int64()) {
				return Int(1), true, nil
			}
			return Int(0), true, nil
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(args[0].String()), 10, 64); err == nil {
			if vm.csv.ExistsID(n) {
				return Int(1), true, nil
			}
			return Int(0), true, nil
		}
		if vm.csv.Exists(args[0].String()) {
			return Int(1), true, nil
		}
		return Int(0), true, nil
	case "GETPALAMLV", "GETEXPLV":
		if len(args) < 1 {
			return Int(0), true, nil
		}
		v := args[0].Int64()
		levels := []int64{0, 100, 500, 3000, 10000, 30000, 60000, 100000, 150000, 250000}
		if name == "GETEXPLV" {
			levels = []int64{0, 1, 4, 20, 50, 200}
		}
		lv := int64(0)
		for i, th := range levels {
			if v >= th {
				lv = int64(i)
			}
		}
		return Int(lv), true, nil
	default:
		return Value{}, false, nil
	}
}

func (vm *VM) htmlColorFromValue(v Value) string {
	if v.Kind() == StringKind {
		s := strings.TrimSpace(v.String())
		s = strings.TrimPrefix(s, "#")
		if s == "" {
			return ""
		}
		if _, err := strconv.ParseInt(s, 16, 64); err == nil {
			return strings.ToUpper(s)
		}
		return s
	}
	n := v.Int64()
	if n < 0 {
		c := strings.TrimSpace(strings.TrimPrefix(vm.ui.Color, "#"))
		if c == "" {
			return ""
		}
		return strings.ToUpper(c)
	}
	return strings.ToUpper(fmt.Sprintf("%06X", n&0xFFFFFF))
}

func (vm *VM) execMethodSumArray(arg string) Value {
	ref, parts, ok := vm.methodArrayRefAndParts(arg, 1)
	if !ok {
		return Int(0)
	}
	arr, ok := vm.lookupArray(strings.ToUpper(ref.Name))
	if !ok || len(arr.Dims) == 0 {
		return Int(0)
	}
	start, end := vm.parseArrayRange(arr.Dims[0], parts, 1, 2)
	sum := int64(0)
	for i := start; i < end; i++ {
		v, _ := arr.Get([]int64{i})
		sum += v.Int64()
	}
	return Int(sum)
}

func (vm *VM) execMethodMatch(arg string) Value {
	ref, parts, ok := vm.methodArrayRefAndParts(arg, 2)
	if !ok {
		return Int(0)
	}
	arr, ok := vm.lookupArray(strings.ToUpper(ref.Name))
	if !ok || len(arr.Dims) == 0 {
		return Int(0)
	}
	target, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return Int(0)
	}
	start, end := vm.parseArrayRange(arr.Dims[0], parts, 2, 3)
	count := int64(0)
	for i := start; i < end; i++ {
		v, _ := arr.Get([]int64{i})
		if valueEqual(v, target) {
			count++
		}
	}
	return Int(count)
}

func (vm *VM) execMethodGroupMatch(args []Value) Value {
	if len(args) < 2 {
		return Int(0)
	}
	base := args[0]
	count := int64(0)
	for _, v := range args[1:] {
		if valueEqual(base, v) {
			count++
		}
	}
	return Int(count)
}

func (vm *VM) execMethodNoSames(args []Value) Value {
	if len(args) < 2 {
		return Int(1)
	}
	base := args[0]
	for _, v := range args[1:] {
		if valueEqual(base, v) {
			return Int(0)
		}
	}
	return Int(1)
}

func (vm *VM) execMethodAllSames(args []Value) Value {
	if len(args) < 2 {
		return Int(1)
	}
	base := args[0]
	for _, v := range args[1:] {
		if !valueEqual(base, v) {
			return Int(0)
		}
	}
	return Int(1)
}

func (vm *VM) execMethodMaxMinArray(arg string, isMax bool) Value {
	ref, parts, ok := vm.methodArrayRefAndParts(arg, 1)
	if !ok {
		return Int(0)
	}
	arr, ok := vm.lookupArray(strings.ToUpper(ref.Name))
	if !ok || len(arr.Dims) == 0 {
		return Int(0)
	}
	start, end := vm.parseArrayRange(arr.Dims[0], parts, 1, 2)
	if start >= end {
		return Int(0)
	}
	first, _ := arr.Get([]int64{start})
	best := first.Int64()
	for i := start + 1; i < end; i++ {
		v, _ := arr.Get([]int64{i})
		n := v.Int64()
		if isMax {
			if n > best {
				best = n
			}
		} else if n < best {
			best = n
		}
	}
	return Int(best)
}

func (vm *VM) execMethodGetNum(arg string, byName bool) Value {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < 2 {
		return Int(-1)
	}
	varName := ""
	if byName {
		v, err := vm.evalLooseExpr(parts[0])
		if err != nil {
			return Int(-1)
		}
		varName = strings.ToUpper(strings.TrimSpace(v.String()))
	} else {
		ref, err := vm.parseVarRefRuntime(parts[0])
		if err != nil {
			return Int(-1)
		}
		varName = strings.ToUpper(strings.TrimSpace(ref.Name))
	}
	keyV, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return Int(-1)
	}
	base := csvBaseFromVarName(varName)
	if base == "" {
		base = varName
	}
	id, ok := vm.csv.FindID(base, keyV.String())
	if !ok {
		return Int(-1)
	}
	return Int(id)
}

func (vm *VM) execMethodFindElement(arg string, last bool) Value {
	ref, parts, ok := vm.methodArrayRefAndParts(arg, 2)
	if !ok {
		return Int(-1)
	}
	arr, ok := vm.lookupArray(strings.ToUpper(ref.Name))
	if !ok || len(arr.Dims) == 0 {
		return Int(-1)
	}
	target, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return Int(-1)
	}
	start, end := vm.parseArrayRange(arr.Dims[0], parts, 2, 3)
	exact := false
	if len(parts) > 4 && strings.TrimSpace(parts[4]) != "" {
		v, err := vm.evalLooseExpr(parts[4])
		if err == nil {
			exact = v.Truthy()
		}
	}
	var re *regexp.Regexp
	if target.Kind() == StringKind && !exact {
		pat := target.String()
		rx, err := regexp.Compile(pat)
		if err == nil {
			re = rx
		}
	}
	if last {
		for i := end - 1; i >= start; i-- {
			v, _ := arr.Get([]int64{i})
			if methodElementMatches(v, target, exact, re) {
				return Int(i)
			}
		}
		return Int(-1)
	}
	for i := start; i < end; i++ {
		v, _ := arr.Get([]int64{i})
		if methodElementMatches(v, target, exact, re) {
			return Int(i)
		}
	}
	return Int(-1)
}

func (vm *VM) execMethodInRangeArray(arg string) Value {
	ref, parts, ok := vm.methodArrayRefAndParts(arg, 3)
	if !ok {
		return Int(0)
	}
	arr, ok := vm.lookupArray(strings.ToUpper(ref.Name))
	if !ok || len(arr.Dims) == 0 {
		return Int(0)
	}
	minV, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return Int(0)
	}
	maxV, err := vm.evalLooseExpr(parts[2])
	if err != nil {
		return Int(0)
	}
	start, end := vm.parseArrayRange(arr.Dims[0], parts, 3, 4)
	count := int64(0)
	lo, hi := minV.Int64(), maxV.Int64()
	for i := start; i < end; i++ {
		v, _ := arr.Get([]int64{i})
		n := v.Int64()
		if n >= lo && n <= hi {
			count++
		}
	}
	return Int(count)
}

func (vm *VM) methodArrayRefAndParts(arg string, minParts int) (ast.VarRef, []string, bool) {
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) < minParts {
		return ast.VarRef{}, nil, false
	}
	ref, err := vm.parseVarRefRuntime(parts[0])
	if err != nil {
		return ast.VarRef{}, nil, false
	}
	if len(ref.Index) != 0 {
		return ast.VarRef{}, nil, false
	}
	return ref, parts, true
}

func (vm *VM) parseArrayRange(length int, parts []string, startPart, endPart int) (int64, int64) {
	n := int64(length)
	start := int64(0)
	end := n
	if startPart < len(parts) && strings.TrimSpace(parts[startPart]) != "" {
		if v, err := vm.evalLooseExpr(parts[startPart]); err == nil {
			start = v.Int64()
		}
	}
	if endPart < len(parts) && strings.TrimSpace(parts[endPart]) != "" {
		if v, err := vm.evalLooseExpr(parts[endPart]); err == nil {
			end = v.Int64()
		}
	}
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > n {
		start = n
	}
	if end > n {
		end = n
	}
	if end < start {
		end = start
	}
	return start, end
}

func methodElementMatches(v Value, target Value, exact bool, re *regexp.Regexp) bool {
	if target.Kind() == StringKind {
		if exact {
			return v.String() == target.String()
		}
		if re != nil {
			return re.MatchString(v.String())
		}
		return v.String() == target.String()
	}
	return v.Int64() == target.Int64()
}

func csvBaseFromVarName(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	switch name {
	case "ABL", "BASE", "MARK", "EXP", "RELATION", "TALENT", "CFLAG", "EQUIP", "JUEL", "CSTR":
		return name
	case "MAXBASE":
		// MAXBASE indices use BASE.CSV labels.
		return "BASE"
	case "NAME":
		return "NAME"
	case "CALLNAME":
		return "CALLNAME"
	case "NICKNAME":
		return "NICKNAME"
	case "MASTERNAME":
		return "MASTERNAME"
	case "ITEM":
		return "ITEM"
	case "TRAIN":
		return "TRAIN"
	default:
		return name
	}
}

func toHalfWidth(s string) string {
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '　':
			runes[i] = ' '
		case r >= '！' && r <= '～':
			runes[i] = r - 0xFEE0
		}
	}
	return string(runes)
}

func toFullWidth(s string) string {
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == ' ':
			runes[i] = '　'
		case r >= '!' && r <= '~':
			runes[i] = r + 0xFEE0
		}
	}
	return string(runes)
}

func isNumericLike(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	re := regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?$`)
	return re.MatchString(s)
}

func (vm *VM) evalVarSizeRaw(raw string) (int64, error) {
	parts := splitTopLevelRuntime(raw, ',')
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return 0, nil
	}
	ref, err := vm.parseVarRefRuntime(parts[0])
	if err != nil {
		return 0, nil
	}
	dimIdx := int64(0)
	if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
		v, err := vm.evalLooseExpr(parts[1])
		if err == nil {
			dimIdx = v.Int64()
		}
	}
	if arr, ok := vm.lookupArray(strings.ToUpper(ref.Name)); ok {
		if dimIdx < 0 || int(dimIdx) >= len(arr.Dims) {
			return 0, nil
		}
		return int64(arr.Dims[dimIdx]), nil
	}
	if dimIdx == 0 {
		return 1, nil
	}
	return 0, nil
}

func (vm *VM) execCallListLike(name, arg string) (execResult, error) {
	if strings.HasPrefix(name, "TRY") {
		if res, ok, err := vm.execTryListBlock(name); ok || err != nil {
			return res, err
		}
	}
	parts := splitTopLevelRuntime(arg, ',')
	if len(parts) == 0 {
		parts = strings.Fields(arg)
	}
	if len(parts) == 0 {
		return execResult{kind: resultNone}, nil
	}
	if name == "CALLEVENT" {
		target := strings.ToUpper(strings.TrimSpace(parts[0]))
		if target == "" {
			return execResult{kind: resultNone}, nil
		}
		if vm.program.Functions[target] == nil {
			return execResult{kind: resultNone}, nil
		}
		return vm.callFunction(target, nil)
	}
	if name == "TRYGOTOLIST" {
		fr := vm.currentFrame()
		for _, p := range parts {
			label := strings.ToUpper(strings.TrimSpace(p))
			if fr != nil {
				if _, ok := fr.fn.Body.LabelMap[label]; ok {
					return execResult{kind: resultGoto, label: label}, nil
				}
			}
		}
		return execResult{kind: resultNone}, nil
	}
	for _, p := range parts {
		target := strings.ToUpper(strings.TrimSpace(p))
		if target == "" || vm.program.Functions[target] == nil {
			continue
		}
		return vm.callFunction(target, nil)
	}
	return execResult{kind: resultNone}, nil
}

func (vm *VM) evalCommandArgs(arg string) ([]Value, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return nil, nil
	}
	exprs, err := parser.ParseExprList(arg)
	if err == nil {
		values := make([]Value, 0, len(exprs))
		for _, e := range exprs {
			v, err := vm.evalExpr(e)
			if err != nil {
				return nil, err
			}
			values = append(values, v)
		}
		return values, nil
	}

	parts := splitTopLevelRuntime(arg, ',')
	values := make([]Value, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		v, err := vm.evalLooseExpr(p)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, nil
}

func parseLooseExpr(raw string) (ast.Expr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ast.StringLit{Value: ""}, nil
	}
	e, err := parser.ParseExpr(raw)
	if err == nil {
		return e, nil
	}
	if v, parseErr := strconv.Unquote(raw); parseErr == nil {
		return ast.StringLit{Value: v}, nil
	}
	return ast.StringLit{Value: raw}, nil
}

func tryUnquoteCommandString(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 3 && strings.HasPrefix(raw, "@\"") && strings.HasSuffix(raw, "\"") {
		return raw[2 : len(raw)-1], true
	}
	if v, err := strconv.Unquote(raw); err == nil {
		return v, true
	}
	return "", false
}

func (vm *VM) evalLooseExpr(raw string) (Value, error) {
	e, err := parseLooseExpr(raw)
	if err != nil {
		return Value{}, err
	}
	return vm.evalExpr(e)
}

func looksLikeCallArgExpr(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if strings.HasPrefix(raw, "@\"") {
		return true
	}
	return strings.ContainsAny(raw, "%@()?:#{}")
}

func (vm *VM) evalCallArgExpr(e ast.Expr) (Value, error) {
	lit, isString := e.(ast.StringLit)
	if !isString {
		return vm.evalExpr(e)
	}
	raw := lit.Value
	if !looksLikeCallArgExpr(raw) {
		return Str(raw), nil
	}
	if v, err := vm.evalLooseExpr(raw); err == nil {
		// parseLooseExpr can silently fall back to a raw string literal.
		// It also trims outer spaces, so compare in trimmed form.
		if v.Kind() != StringKind || strings.TrimSpace(v.String()) != strings.TrimSpace(raw) {
			return v, nil
		}
	}
	if text, ok, err := vm.evalCallArgFormString(raw); err != nil {
		return Value{}, err
	} else if ok {
		return Str(text), nil
	}
	if text, err := vm.expandDecodedTemplate(raw); err == nil {
		return Str(text), nil
	}
	return Str(raw), nil
}

func (vm *VM) evalCallArgFormString(raw string) (string, bool, error) {
	parts := splitTopLevelRuntime(raw, '+')
	if len(parts) == 0 {
		return "", false, nil
	}
	var b strings.Builder
	handled := false
	for _, p := range parts {
		part := strings.TrimSpace(p)
		if part == "" {
			continue
		}
		text, ok, err := vm.evalCallArgFormTerm(part)
		if err != nil {
			return "", false, err
		}
		if !ok {
			return "", false, nil
		}
		handled = true
		b.WriteString(text)
	}
	if !handled {
		return "", false, nil
	}
	return b.String(), true, nil
}

func (vm *VM) evalCallArgFormTerm(part string) (string, bool, error) {
	part = strings.TrimSpace(part)
	if part == "" {
		return "", true, nil
	}
	if strings.HasPrefix(part, "@\"") && strings.HasSuffix(part, "\"") && len(part) >= 3 {
		inner := part[2 : len(part)-1]
		text, err := vm.expandDecodedTemplate(inner)
		if err != nil {
			return "", false, err
		}
		return text, true, nil
	}
	if strings.HasPrefix(part, "@") && strings.HasSuffix(part, "@") && len(part) >= 2 {
		inner := strings.TrimSpace(part[1 : len(part)-1])
		if strings.HasSuffix(inner, "#") {
			inner += ` ""`
		}
		text, ok, err := vm.evalAtPlaceholderExpr(inner)
		if err != nil {
			return "", false, err
		}
		if ok {
			return text, true, nil
		}
	}
	if uq, ok := tryUnquoteCommandString(part); ok {
		text, err := vm.expandDecodedTemplate(uq)
		if err != nil {
			return "", false, err
		}
		return text, true, nil
	}
	if v, err := vm.evalLooseExpr(part); err == nil {
		if v.Kind() != StringKind || v.String() != part {
			return v.String(), true, nil
		}
	}
	if text, err := vm.expandDecodedTemplate(part); err == nil && text != part {
		return text, true, nil
	}
	return "", false, nil
}

func (vm *VM) expandDecodedTemplate(raw string) (string, error) {
	return vm.expandFormTemplate(decodeCommandCharSeq(raw))
}

func (vm *VM) parseVarRefRuntime(raw string) (ast.VarRef, error) {
	e, err := parser.ParseExpr(strings.TrimSpace(raw))
	if err != nil {
		return ast.VarRef{}, err
	}
	ref, ok := e.(ast.VarRef)
	if !ok {
		return ast.VarRef{}, fmt.Errorf("not a variable reference")
	}
	return ref, nil
}

func (vm *VM) evalExprList(raw string) ([]Value, error) {
	exprList, err := parser.ParseExprList(raw)
	if err != nil {
		return nil, err
	}
	values := make([]Value, 0, len(exprList))
	for _, expr := range exprList {
		v, err := vm.evalExpr(expr)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, nil
}

func (vm *VM) evalCommandTarget(raw string, dynamic bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if dynamic {
		target, err := vm.evalDynamicTarget(raw)
		if err != nil {
			return "", err
		}
		return strings.ToUpper(strings.TrimSpace(target)), nil
	}
	cmd, _ := splitNameAndRestRuntime(raw)
	return strings.ToUpper(strings.TrimSpace(cmd)), nil
}

func (vm *VM) parseCommandCall(raw string, dynamic bool) (string, []Value, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, nil
	}
	if dynamic {
		if targetRaw, argRaw, ok := splitDynamicTargetCall(raw); ok {
			targetText, err := vm.evalDynamicTarget(targetRaw)
			if err != nil {
				return "", nil, err
			}
			target := strings.ToUpper(strings.TrimSpace(targetText))
			args, err := vm.evalDynamicCallArgs(argRaw)
			if err != nil {
				return "", nil, err
			}
			return target, args, nil
		}

		parts := splitTopLevelRuntime(raw, ',')
		if len(parts) == 0 {
			return "", nil, nil
		}
		targetText, err := vm.evalDynamicTarget(parts[0])
		if err != nil {
			return "", nil, err
		}
		target := strings.ToUpper(strings.TrimSpace(targetText))
		args := make([]Value, 0, max(0, len(parts)-1))
		for _, p := range parts[1:] {
			if strings.TrimSpace(p) == "" {
				continue
			}
			v, err := vm.evalCallArgRaw(p)
			if err != nil {
				return "", nil, err
			}
			args = append(args, v)
		}
		return target, args, nil
	}

	if i := strings.Index(raw, "("); i >= 0 && strings.HasSuffix(raw, ")") {
		target := strings.ToUpper(strings.TrimSpace(raw[:i]))
		argRaw := strings.TrimSpace(raw[i+1 : len(raw)-1])
		values, err := vm.evalCallArgList(argRaw)
		if err != nil {
			return "", nil, err
		}
		return target, values, nil
	}

	parts := splitTopLevelRuntime(raw, ',')
	if len(parts) == 0 {
		return "", nil, nil
	}
	target := strings.ToUpper(strings.TrimSpace(parts[0]))
	args := make([]Value, 0, max(0, len(parts)-1))
	for _, p := range parts[1:] {
		if strings.TrimSpace(p) == "" {
			continue
		}
		v, err := vm.evalCallArgRaw(p)
		if err != nil {
			return "", nil, err
		}
		args = append(args, v)
	}
	return target, args, nil
}

func (vm *VM) evalDynamicCallArgs(argRaw string) ([]Value, error) {
	argRaw = strings.TrimSpace(argRaw)
	if argRaw == "" {
		return nil, nil
	}
	values, err := vm.evalCallArgList(argRaw)
	if err == nil {
		return values, nil
	}
	parts := splitTopLevelRuntime(argRaw, ',')
	values = make([]Value, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := vm.evalCallArgRaw(p)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, nil
}

func (vm *VM) evalCallArgList(raw string) ([]Value, error) {
	exprList, err := parser.ParseExprList(raw)
	if err != nil {
		return nil, err
	}
	values := make([]Value, 0, len(exprList))
	for _, e := range exprList {
		v, err := vm.evalCallArgExpr(e)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, nil
}

func (vm *VM) evalCallArgRaw(raw string) (Value, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Str(""), nil
	}
	if expr, err := parser.ParseExpr(raw); err == nil {
		return vm.evalCallArgExpr(expr)
	}
	return vm.evalCallArgExpr(ast.StringLit{Value: raw})
}

func (vm *VM) evalDynamicTarget(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if v, err := vm.evalLooseExpr(raw); err == nil {
		s := strings.TrimSpace(v.String())
		// parseLooseExpr may fallback to raw string; in that case try form expansion.
		if !(s == raw && (strings.Contains(raw, "%") || strings.Contains(raw, "{"))) {
			return s, nil
		}
	}
	text := decodeCommandCharSeq(raw)
	for i := 0; i < 6; i++ {
		prev := text
		if t, err := vm.evalPercentPlaceholders(text); err == nil {
			text = t
		}
		if t, err := vm.evalBracePlaceholders(text); err == nil {
			text = t
		}
		if text == prev {
			break
		}
	}
	return strings.TrimSpace(text), nil
}

func shouldNewlineOnPrint(name string) bool {
	if strings.HasPrefix(name, "PRINTL") || strings.HasPrefix(name, "DEBUGPRINTL") {
		return true
	}
	if strings.HasSuffix(name, "L") {
		return true
	}
	return false
}

func isAny(name string, candidates ...string) bool {
	for _, c := range candidates {
		if name == c {
			return true
		}
	}
	return false
}

func splitNameAndRestRuntime(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	for i, r := range raw {
		if unicode.IsSpace(r) {
			return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+1:])
		}
	}
	for i, r := range raw {
		if i == 0 {
			continue
		}
		if !runtimeIdentPart(r) {
			return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i:])
		}
	}
	return raw, ""
}

func runtimeIdentPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '・' || r == '·'
}

func splitDynamicTargetCall(raw string) (targetRaw string, argRaw string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	inStr := false
	escape := false
	depth := 0
	start := -1
	for i, r := range raw {
		if inStr {
			if escape {
				escape = false
				continue
			}
			if r == '\\' {
				escape = true
				continue
			}
			if r == '"' {
				inStr = false
			}
			continue
		}
		switch r {
		case '"':
			inStr = true
		case '(':
			if depth == 0 {
				start = i
			}
			depth++
		case ')':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				if strings.TrimSpace(raw[i+1:]) != "" {
					return "", "", false
				}
				return strings.TrimSpace(raw[:start]), strings.TrimSpace(raw[start+1 : i]), true
			}
		}
	}
	return "", "", false
}

func splitTopLevelRuntime(raw string, sep rune) []string {
	parts := []string{}
	depth := 0
	inStr := false
	escape := false
	start := 0
	for i, r := range raw {
		if inStr {
			if escape {
				escape = false
				continue
			}
			if r == '\\' {
				escape = true
				continue
			}
			if r == '"' {
				inStr = false
			}
			continue
		}
		switch r {
		case '"':
			inStr = true
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if r == sep && depth == 0 {
				parts = append(parts, strings.TrimSpace(raw[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(raw[start:]))
	return parts
}

func (vm *VM) currentFrame() *frame {
	if len(vm.stack) == 0 {
		return nil
	}
	return vm.stack[len(vm.stack)-1]
}

func isResultLikeName(name string) bool {
	name = strings.ToUpper(strings.TrimSpace(name))
	return name == "RESULT" || name == "RESULTS"
}

func (vm *VM) setVar(name string, v Value) {
	name = strings.ToUpper(name)
	if bound, ok := vm.resolveRefBinding(name); ok {
		_ = vm.setVarRef(bound, v)
		return
	}
	if isResultLikeName(name) {
		vm.globals[name] = v
		if fr := vm.currentFrame(); fr != nil {
			if arr, ok := fr.lArrays[name]; ok {
				_ = arr.Set([]int64{0}, v)
				return
			}
		}
		if arr, ok := vm.gArrays[name]; ok {
			_ = arr.Set([]int64{0}, v)
		}
		return
	}
	if fr := vm.currentFrame(); fr != nil {
		if _, ok := fr.locals[name]; ok {
			fr.locals[name] = v
			return
		}
		if arr, ok := fr.lArrays[name]; ok {
			_ = arr.Set([]int64{0}, v)
			return
		}
	}
	if arr, ok := vm.gArrays[name]; ok {
		_ = arr.Set([]int64{0}, v)
		return
	}
	vm.globals[name] = v
}

func (vm *VM) getVar(name string) Value {
	name = strings.ToUpper(name)
	if name == "LINECOUNT" {
		return Int(int64(len(vm.outputs)))
	}
	if bound, ok := vm.resolveRefBinding(name); ok {
		v, err := vm.getVarRef(bound)
		if err == nil {
			return v
		}
	}
	if isResultLikeName(name) {
		if v, ok := vm.globals[name]; ok {
			return v
		}
	}
	if fr := vm.currentFrame(); fr != nil {
		if v, ok := fr.locals[name]; ok {
			return v
		}
		if arr, ok := fr.lArrays[name]; ok {
			v, err := arr.Get([]int64{0})
			if err == nil {
				return v
			}
		}
	}
	if arr, ok := vm.gArrays[name]; ok {
		v, err := arr.Get([]int64{0})
		if err == nil {
			return v
		}
	}
	if v, ok := vm.globals[name]; ok {
		return v
	}
	return Int(0)
}

func (vm *VM) isStringArrayBase(name string) bool {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	if _, ok := vm.program.StringVars[name]; ok {
		return true
	}
	switch name {
	case "NAME", "CALLNAME", "NICKNAME", "MASTERNAME", "CSTR", "LOCALS", "ARGS", "RESULTS", "GLOBALS":
		return true
	default:
		return false
	}
}

func (vm *VM) isCharacterTextBase(name string) bool {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "NAME", "CALLNAME", "NICKNAME", "MASTERNAME":
		return true
	default:
		return false
	}
}

func (vm *VM) characterIDByIndex(index int64) (int64, bool) {
	if index < 0 || index >= int64(len(vm.characters)) {
		return 0, false
	}
	return vm.characters[int(index)].ID, true
}

func (vm *VM) characterTextFallback(name string, index []int64) (Value, bool) {
	if !vm.isCharacterTextBase(name) || len(index) == 0 {
		return Value{}, false
	}
	id, ok := vm.characterIDByIndex(index[0])
	if !ok {
		return Str(""), true
	}
	if v, found := vm.csv.Name(strings.ToUpper(name), id); found {
		return Str(v), true
	}
	return Str(""), true
}

func arrayHasExplicitValue(arr *ArrayVar, index []int64) bool {
	if arr == nil {
		return false
	}
	k, err := arr.key(index)
	if err != nil {
		return false
	}
	_, ok := arr.Data[k]
	return ok
}

func (vm *VM) getVarRef(ref ast.VarRef) (Value, error) {
	name := strings.ToUpper(ref.Name)
	if len(ref.Index) == 0 {
		if bound, ok := vm.resolveRefBinding(name); ok {
			return vm.getVarRef(bound)
		}
		return vm.getVar(name), nil
	}
	if name == "RAND" {
		limitExpr := ref.Index[0]
		limitVal, err := vm.evalExpr(limitExpr)
		if err != nil {
			return Value{}, err
		}
		n := limitVal.Int64()
		if n <= 0 {
			return Int(0), nil
		}
		return Int(vm.rng.Int63n(n)), nil
	}
	index, err := vm.evalIndexExprsFor(name, ref.Index)
	if err != nil {
		return Value{}, err
	}
	if isResultLikeName(name) && len(index) == 1 && index[0] == 0 {
		if v, ok := vm.globals[name]; ok {
			return v, nil
		}
	}
	if name == "NO" && len(index) > 0 {
		if id, ok := vm.characterIDByIndex(index[0]); ok {
			return Int(id), nil
		}
		return Int(0), nil
	}
	if fr := vm.currentFrame(); fr != nil {
		if arr, ok := fr.lArrays[name]; ok {
			v, err := arr.Get(index)
			if err != nil {
				if arr.IsString && hasNegativeIndex(index) {
					return Str(""), nil
				}
				return Value{}, fmt.Errorf("%s:%v: %w", name, index, err)
			}
			if !arrayHasExplicitValue(arr, index) {
				if fb, ok := vm.characterTextFallback(name, index); ok {
					return fb, nil
				}
			}
			return v, nil
		}
	}
	if arr, ok := vm.gArrays[name]; ok {
		v, err := arr.Get(index)
		if err != nil {
			if arr.IsString && hasNegativeIndex(index) {
				return Str(""), nil
			}
			return Value{}, fmt.Errorf("%s:%v: %w", name, index, err)
		}
		if !arrayHasExplicitValue(arr, index) {
			if fb, ok := vm.characterTextFallback(name, index); ok {
				return fb, nil
			}
		}
		return v, nil
	}
	if fb, ok := vm.characterTextFallback(name, index); ok {
		return fb, nil
	}
	if fr := vm.currentFrame(); fr != nil && strings.HasPrefix(name, "LOCAL") {
		arr := newArrayVar(vm.isStringArrayBase(name), true, dimsForIndex(index))
		fr.lArrays[name] = arr
		v, err := arr.Get(index)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%v: %w", name, index, err)
		}
		return v, nil
	}
	arr := newArrayVar(vm.isStringArrayBase(name), true, dimsForIndex(index))
	vm.gArrays[name] = arr
	v, err := arr.Get(index)
	if err != nil {
		return Value{}, fmt.Errorf("%s:%v: %w", name, index, err)
	}
	return v, nil
}

func (vm *VM) setVarRef(ref ast.VarRef, v Value) error {
	name := strings.ToUpper(ref.Name)
	if len(ref.Index) == 0 {
		if bound, ok := vm.resolveRefBinding(name); ok {
			return vm.setVarRef(bound, v)
		}
		vm.setVar(name, v)
		return nil
	}
	index, err := vm.evalIndexExprsFor(name, ref.Index)
	if err != nil {
		return err
	}
	if name == "NO" && len(index) > 0 && index[0] >= 0 && index[0] < int64(len(vm.characters)) {
		vm.characters[int(index[0])].ID = v.Int64()
		return nil
	}
	if fr := vm.currentFrame(); fr != nil {
		if arr, ok := fr.lArrays[name]; ok {
			if err := arr.Set(index, v); err != nil {
				return fmt.Errorf("%s:%v: %w", name, index, err)
			}
			if isResultLikeName(name) && len(index) == 1 && index[0] == 0 {
				vm.globals[name] = v
			}
			return nil
		}
	}
	arr := vm.gArrays[name]
	if arr == nil {
		arr = newArrayVar(vm.isStringArrayBase(name), true, dimsForIndex(index))
		vm.gArrays[name] = arr
	}
	if err := arr.Set(index, v); err != nil {
		return fmt.Errorf("%s:%v: %w", name, index, err)
	}
	if isResultLikeName(name) && len(index) == 1 && index[0] == 0 {
		vm.globals[name] = v
	}
	return nil
}

func (vm *VM) defaultValueForVarRef(ref ast.VarRef) (Value, error) {
	name := strings.ToUpper(strings.TrimSpace(ref.Name))
	if len(ref.Index) == 0 {
		if fr := vm.currentFrame(); fr != nil {
			if v, ok := fr.locals[name]; ok {
				if v.Kind() == StringKind {
					return Str(""), nil
				}
				return Int(0), nil
			}
			if arr, ok := fr.lArrays[name]; ok {
				return arr.defaultValue(), nil
			}
		}
		if arr, ok := vm.gArrays[name]; ok {
			return arr.defaultValue(), nil
		}
		if v, ok := vm.globals[name]; ok && v.Kind() == StringKind {
			return Str(""), nil
		}
		if _, ok := vm.program.StringVars[name]; ok {
			return Str(""), nil
		}
		return Int(0), nil
	}

	if fr := vm.currentFrame(); fr != nil {
		if arr, ok := fr.lArrays[name]; ok {
			return arr.defaultValue(), nil
		}
	}
	if arr, ok := vm.gArrays[name]; ok {
		return arr.defaultValue(), nil
	}
	if vm.isStringArrayBase(name) {
		return Str(""), nil
	}
	return Int(0), nil
}

func (vm *VM) normalizeFuncArgTarget(arg ast.Arg, position int) ast.VarRef {
	target := arg.Target
	if strings.TrimSpace(target.Name) == "" {
		target = ast.VarRef{Name: arg.Name}
	}
	target.Name = strings.ToUpper(strings.TrimSpace(target.Name))
	if len(target.Index) == 0 && (target.Name == "ARG" || target.Name == "ARGS") {
		target.Index = []ast.Expr{ast.IntLit{Value: int64(position)}}
	}
	return target
}

func hasNegativeIndex(index []int64) bool {
	for _, v := range index {
		if v < 0 {
			return true
		}
	}
	return false
}

func (vm *VM) defaultValueForFuncArgTarget(target ast.VarRef) Value {
	name := strings.ToUpper(strings.TrimSpace(target.Name))
	if vm.isStringArrayBase(name) {
		return Str("")
	}
	if _, ok := vm.program.StringVars[name]; ok {
		return Str("")
	}
	return Int(0)
}

func (vm *VM) isRefDeclared(name string) bool {
	name = strings.ToUpper(name)
	if fr := vm.currentFrame(); fr != nil {
		if fr.lRefDecl[name] {
			return true
		}
	}
	return vm.gRefDecl[name]
}

func (vm *VM) setRefBinding(name string, target ast.VarRef) {
	name = strings.ToUpper(name)
	target.Name = strings.ToUpper(target.Name)
	if fr := vm.currentFrame(); fr != nil && fr.lRefDecl[name] {
		fr.refs[name] = target
		return
	}
	if vm.gRefDecl[name] {
		vm.gRefs[name] = target
	}
}

func (vm *VM) resolveRefBinding(name string) (ast.VarRef, bool) {
	name = strings.ToUpper(name)
	if fr := vm.currentFrame(); fr != nil {
		if t, ok := fr.refs[name]; ok {
			return t, true
		}
	}
	t, ok := vm.gRefs[name]
	return t, ok
}

func (vm *VM) evalIndexExprs(exprs []ast.Expr) ([]int64, error) {
	return vm.evalIndexExprsFor("", exprs)
}

func dimsForIndex(index []int64) []int {
	dims := make([]int, len(index))
	for i, idx := range index {
		d := int(idx) + 1
		if d < 1 {
			d = 1
		}
		dims[i] = d
	}
	return dims
}

func (vm *VM) evalIndexExprsFor(baseName string, exprs []ast.Expr) ([]int64, error) {
	idx := make([]int64, 0, len(exprs))
	csvBase := csvBaseFromVarName(baseName)
	for _, expr := range exprs {
		if mapped, ok := vm.resolveNamedCSVIndex(baseName, expr); ok {
			idx = append(idx, mapped)
			continue
		}
		v, err := vm.evalExpr(expr)
		if err != nil {
			return nil, err
		}
		if mapped, ok := vm.resolveNamedCSVIndexValue(csvBase, v); ok {
			idx = append(idx, mapped)
			continue
		}
		idx = append(idx, v.Int64())
	}
	return idx, nil
}

func (vm *VM) resolveNamedCSVIndex(baseName string, expr ast.Expr) (int64, bool) {
	baseName = strings.ToUpper(strings.TrimSpace(baseName))
	if baseName == "" {
		return 0, false
	}
	ref, ok := expr.(ast.VarRef)
	if !ok || len(ref.Index) != 0 {
		return 0, false
	}
	key := strings.TrimSpace(ref.Name)
	if key == "" {
		return 0, false
	}
	if vm.symbolExists(key) {
		return 0, false
	}
	if id, ok := vm.csv.FindID(csvBaseFromVarName(baseName), key); ok {
		return id, true
	}
	return 0, false
}

func (vm *VM) resolveNamedCSVIndexValue(csvBase string, v Value) (int64, bool) {
	csvBase = strings.ToUpper(strings.TrimSpace(csvBase))
	if csvBase == "" || v.Kind() != StringKind {
		return 0, false
	}
	key := strings.TrimSpace(v.String())
	if key == "" || isNumericLike(key) {
		return 0, false
	}
	if id, ok := vm.csv.FindID(csvBase, key); ok {
		return id, true
	}
	return 0, false
}

func (vm *VM) symbolExists(name string) bool {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	if _, ok := vm.resolveRefBinding(name); ok {
		return true
	}
	if fr := vm.currentFrame(); fr != nil {
		if _, ok := fr.locals[name]; ok {
			return true
		}
		if _, ok := fr.lArrays[name]; ok {
			return true
		}
		if fr.lRefDecl[name] {
			return true
		}
		if _, ok := fr.refs[name]; ok {
			return true
		}
	}
	if _, ok := vm.globals[name]; ok {
		return true
	}
	if _, ok := vm.gArrays[name]; ok {
		return true
	}
	if vm.gRefDecl[name] {
		return true
	}
	if _, ok := vm.gRefs[name]; ok {
		return true
	}
	if _, ok := vm.program.StringVars[name]; ok {
		return true
	}
	return false
}

func (vm *VM) storeResult(values []Value) {
	if len(values) == 0 {
		vm.globals["RESULT"] = Int(0)
		return
	}
	vm.globals["RESULT"] = values[0]
	for i, v := range values {
		vm.globals[fmt.Sprintf("RESULT%d", i)] = v
	}
}
