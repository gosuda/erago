package eruntime

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gosuda/erago/ast"
	"github.com/gosuda/erago/parser"
)

type Output struct {
	Text    string
	NewLine bool
}

type VM struct {
	program    *ast.Program
	globals    map[string]Value
	gArrays    map[string]*ArrayVar
	gRefDecl   map[string]bool
	gRefs      map[string]ast.VarRef
	stack      []*frame
	outputs    []Output
	rng        *rand.Rand
	csv        *CSVStore
	saveDir    string
	ui         UIState
	characters []RuntimeCharacter
	nextCharID int64
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
	resultBegin
	resultReturn
	resultQuit
	resultBreak
	resultContinue
)

type execResult struct {
	kind    resultKind
	label   string
	keyword string
	values  []Value
}

func New(program *ast.Program) (*VM, error) {
	vm := &VM{
		program:    program,
		globals:    map[string]Value{},
		gArrays:    map[string]*ArrayVar{},
		gRefDecl:   map[string]bool{},
		gRefs:      map[string]ast.VarRef{},
		stack:      nil,
		outputs:    nil,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
		csv:        newCSVStore(program.CSVFiles),
		saveDir:    filepath.Join(".", ".erago_saves"),
		ui:         defaultUIState(),
		characters: nil,
		nextCharID: 0,
	}
	if err := vm.initDefines(); err != nil {
		return nil, err
	}
	return vm, nil
}

func (vm *VM) initDefines() error {
	keys := make([]string, 0, len(vm.program.Defines))
	for k := range vm.program.Defines {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, err := vm.evalExpr(vm.program.Defines[k])
		if err != nil {
			return fmt.Errorf("define %s: %w", k, err)
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
	for name := range vm.program.StringVars {
		if _, ok := vm.globals[name]; !ok && vm.gArrays[name] == nil {
			vm.globals[name] = Str("")
		}
	}
	if _, ok := vm.globals["RESULT"]; !ok {
		vm.globals["RESULT"] = Int(0)
	}
	return nil
}

func (vm *VM) Run(entry string) ([]Output, error) {
	vm.outputs = vm.outputs[:0]
	vm.ui = defaultUIState()
	vm.characters = nil
	vm.nextCharID = 0
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
			current = strings.ToUpper(res.keyword)
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
		if i < len(args) {
			fr.locals[arg.Name] = args[i]
			continue
		}
		if arg.Default != nil {
			v, err := vm.evalExpr(arg.Default)
			if err != nil {
				return execResult{}, fmt.Errorf("%s default arg %s: %w", fn.Name, arg.Name, err)
			}
			fr.locals[arg.Name] = v
			continue
		}
		fr.locals[arg.Name] = Int(0)
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
	for pc := 0; pc < len(thunk.Statements); pc++ {
		stmt := thunk.Statements[pc]
		res, err := vm.runStatement(stmt)
		if err != nil {
			return execResult{}, err
		}
		if res.kind == resultGoto {
			idx, ok := thunk.LabelMap[strings.ToUpper(res.label)]
			if ok {
				pc = idx - 1
				continue
			}
			return res, nil
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
		vm.outputs = append(vm.outputs, Output{Text: v.String(), NewLine: s.NewLine})
		return execResult{kind: resultNone}, nil
	case ast.AssignStmt:
		if s.Op == "=" && len(s.Target.Index) == 0 {
			if sourceRef, ok := s.Expr.(ast.VarRef); ok && vm.isRefDeclared(s.Target.Name) {
				vm.setRefBinding(strings.ToUpper(s.Target.Name), sourceRef)
				return execResult{kind: resultNone}, nil
			}
		}
		v, err := vm.evalExpr(s.Expr)
		if err != nil {
			return execResult{}, err
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
		varName := strings.ToUpper(s.Var)
		step := stepVal.Int64()
		if step == 0 {
			step = 1
		}
		vm.setVar(varName, Int(initVal.Int64()))
		for {
			cur := vm.getVar(varName).Int64()
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
			vm.setVar(varName, Int(vm.getVar(varName).Int64()+step))
		}
	case ast.GotoStmt:
		return execResult{kind: resultGoto, label: strings.ToUpper(s.Label)}, nil
	case ast.CallStmt:
		args := make([]Value, 0, len(s.Args))
		for _, e := range s.Args {
			v, err := vm.evalExpr(e)
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
		vm.outputs = append(vm.outputs, Output{
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
			vm.outputs = append(vm.outputs, Output{Text: text, NewLine: shouldNewlineOnPrint(name)})
		}
		return execResult{kind: resultNone}, nil
	}

	switch name {
	case "WAIT", "WAITANYKEY", "FORCEWAIT":
		return execResult{kind: resultNone}, nil
	case "INPUT", "ONEINPUT", "TINPUT", "TONEINPUT":
		vm.globals["RESULT"] = Int(0)
		return execResult{kind: resultNone}, nil
	case "INPUTS", "ONEINPUTS", "TINPUTS", "TONEINPUTS":
		vm.globals["RESULT"] = Str("")
		return execResult{kind: resultNone}, nil
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
	case "RETURNF":
		values, err := vm.evalExprList(arg)
		if err != nil {
			return execResult{}, err
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
	case "SKIPDISP", "MOUSESKIP":
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
	case "ADDCHARA", "ADDDEFCHARA", "ADDVOIDCHARA":
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
	case "OUTPUTLOG":
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "DEBUGCLEAR":
		vm.outputs = vm.outputs[:0]
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "RESETGLOBAL":
		return vm.execResetGlobal()
	case "RESETDATA":
		return vm.execResetData()
	case "RESET_STAIN", "STOPCALLTRAIN", "CBGCLEAR", "CBGCLEARBUTTON", "CBGREMOVEBMAP", "CLEARTEXTBOX", "UPCHECK", "CUPCHECK":
		vm.globals["RESULT"] = Int(1)
		return execResult{kind: resultNone}, nil
	case "GOTO", "GOTOFORM", "TRYGOTO", "TRYGOTOFORM", "TRYCGOTO", "TRYCGOTOFORM":
		label, err := vm.evalCommandTarget(arg, strings.Contains(name, "FORM"))
		if err != nil {
			if strings.HasPrefix(name, "TRY") {
				return execResult{kind: resultNone}, nil
			}
			return execResult{}, err
		}
		if label == "" {
			if strings.HasPrefix(name, "TRY") {
				return execResult{kind: resultNone}, nil
			}
			return execResult{}, fmt.Errorf("%s without target", name)
		}
		if strings.HasPrefix(name, "TRY") {
			fr := vm.currentFrame()
			if fr != nil {
				if _, ok := fr.fn.Body.LabelMap[strings.ToUpper(label)]; !ok {
					return execResult{kind: resultNone}, nil
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
				return execResult{kind: resultNone}, nil
			}
			return execResult{}, err
		}
		if target == "" {
			if strings.HasPrefix(name, "TRY") {
				return execResult{kind: resultNone}, nil
			}
			return execResult{}, fmt.Errorf("%s without target", name)
		}
		if vm.program.Functions[target] == nil {
			if strings.HasPrefix(name, "TRY") {
				return execResult{kind: resultNone}, nil
			}
			return execResult{}, fmt.Errorf("function %s not found", target)
		}
		return vm.callFunction(target, args)
	}

	return execResult{kind: resultNone}, nil
}

func (vm *VM) evalCommandPrint(name, arg string) (string, error) {
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

func (vm *VM) evalPrintForms(arg string) (string, error) {
	v, err := vm.evalLooseExpr(arg)
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
		v, err := vm.evalLooseExpr(p)
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
	if len(parts) < 2 {
		return execResult{}, fmt.Errorf("VARSET/CVARSET requires variable and value")
	}
	target, err := vm.parseVarRefRuntime(parts[0])
	if err != nil {
		return execResult{}, fmt.Errorf("VARSET/CVARSET invalid variable: %w", err)
	}
	val, err := vm.evalLooseExpr(parts[1])
	if err != nil {
		return execResult{}, err
	}
	if err := vm.setVarRef(target, val); err != nil {
		return execResult{}, err
	}
	vm.globals["RESULT"] = Int(1)
	return execResult{kind: resultNone}, nil
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
	baseIdx, err := vm.evalIndexExprs(dest.Index)
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
		vm.outputs = append(vm.outputs, Output{Text: text, NewLine: name == "BARL"})
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
	vm.outputs = append(vm.outputs, Output{Text: text, NewLine: true})
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
	vm.outputs = append(vm.outputs, last)
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
	if vm.ui.Redraw {
		vm.globals["RESULT"] = Int(1)
	} else {
		vm.globals["RESULT"] = Int(0)
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

func (vm *VM) execMethodLike(name, arg string) (Value, bool, error) {
	args, err := vm.evalCommandArgs(arg)
	if err != nil {
		return Value{}, false, err
	}
	switch name {
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
	case "EXISTCSV":
		if len(args) < 1 {
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

func (vm *VM) evalLooseExpr(raw string) (Value, error) {
	e, err := parseLooseExpr(raw)
	if err != nil {
		return Value{}, err
	}
	return vm.evalExpr(e)
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
		v, err := vm.evalLooseExpr(raw)
		if err != nil {
			return "", err
		}
		return strings.ToUpper(strings.TrimSpace(v.String())), nil
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
		parts := splitTopLevelRuntime(raw, ',')
		if len(parts) == 0 {
			return "", nil, nil
		}
		targetVal, err := vm.evalLooseExpr(parts[0])
		if err != nil {
			return "", nil, err
		}
		target := strings.ToUpper(strings.TrimSpace(targetVal.String()))
		args := make([]Value, 0, max(0, len(parts)-1))
		for _, p := range parts[1:] {
			if strings.TrimSpace(p) == "" {
				continue
			}
			v, err := vm.evalLooseExpr(p)
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
		values, err := vm.evalExprList(argRaw)
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
		v, err := vm.evalLooseExpr(p)
		if err != nil {
			return "", nil, err
		}
		args = append(args, v)
	}
	return target, args, nil
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
		if r == ' ' || r == '\t' {
			return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+1:])
		}
	}
	return raw, ""
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

func (vm *VM) setVar(name string, v Value) {
	name = strings.ToUpper(name)
	if bound, ok := vm.resolveRefBinding(name); ok {
		_ = vm.setVarRef(bound, v)
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
	if bound, ok := vm.resolveRefBinding(name); ok {
		v, err := vm.getVarRef(bound)
		if err == nil {
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

func (vm *VM) getVarRef(ref ast.VarRef) (Value, error) {
	name := strings.ToUpper(ref.Name)
	if len(ref.Index) == 0 {
		if bound, ok := vm.resolveRefBinding(name); ok {
			return vm.getVarRef(bound)
		}
		return vm.getVar(name), nil
	}
	index, err := vm.evalIndexExprs(ref.Index)
	if err != nil {
		return Value{}, err
	}
	if fr := vm.currentFrame(); fr != nil {
		if arr, ok := fr.lArrays[name]; ok {
			return arr.Get(index)
		}
	}
	if arr, ok := vm.gArrays[name]; ok {
		return arr.Get(index)
	}
	return Value{}, fmt.Errorf("array variable %s is not declared", name)
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
	index, err := vm.evalIndexExprs(ref.Index)
	if err != nil {
		return err
	}
	if fr := vm.currentFrame(); fr != nil {
		if arr, ok := fr.lArrays[name]; ok {
			return arr.Set(index, v)
		}
	}
	arr := vm.gArrays[name]
	if arr == nil {
		// Auto-create int array for dynamic use when not declared.
		dims := make([]int, len(index))
		for i, idx := range index {
			dims[i] = int(idx) + 1
			if dims[i] < 1 {
				dims[i] = 1
			}
		}
		arr = newArrayVar(false, true, dims)
		vm.gArrays[name] = arr
	}
	return arr.Set(index, v)
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
	idx := make([]int64, 0, len(exprs))
	for _, expr := range exprs {
		v, err := vm.evalExpr(expr)
		if err != nil {
			return nil, err
		}
		idx = append(idx, v.Int64())
	}
	return idx, nil
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

func (vm *VM) evalExpr(e ast.Expr) (Value, error) {
	switch ex := e.(type) {
	case ast.IntLit:
		return Int(ex.Value), nil
	case ast.StringLit:
		return Str(ex.Value), nil
	case ast.VarRef:
		return vm.getVarRef(ex)
	case ast.UnaryExpr:
		v, err := vm.evalExpr(ex.Expr)
		if err != nil {
			return Value{}, err
		}
		switch ex.Op {
		case "+":
			return Int(v.Int64()), nil
		case "-":
			return Int(-v.Int64()), nil
		case "!":
			if v.Truthy() {
				return Int(0), nil
			}
			return Int(1), nil
		case "~":
			return Int(^v.Int64()), nil
		default:
			return Value{}, fmt.Errorf("unsupported unary operator %q", ex.Op)
		}
	case ast.BinaryExpr:
		left, err := vm.evalExpr(ex.Left)
		if err != nil {
			return Value{}, err
		}
		right, err := vm.evalExpr(ex.Right)
		if err != nil {
			return Value{}, err
		}
		return evalBinary(ex.Op, left, right)
	default:
		return Value{}, fmt.Errorf("unsupported expression %T", e)
	}
}

func evalBinary(op string, left, right Value) (Value, error) {
	switch op {
	case "+":
		if left.Kind() == StringKind || right.Kind() == StringKind {
			return Str(left.String() + right.String()), nil
		}
		return Int(left.Int64() + right.Int64()), nil
	case "-":
		return Int(left.Int64() - right.Int64()), nil
	case "*":
		return Int(left.Int64() * right.Int64()), nil
	case "/":
		if right.Int64() == 0 {
			return Value{}, fmt.Errorf("division by zero")
		}
		return Int(left.Int64() / right.Int64()), nil
	case "%":
		if right.Int64() == 0 {
			return Value{}, fmt.Errorf("modulo by zero")
		}
		return Int(left.Int64() % right.Int64()), nil
	case "<<":
		return Int(left.Int64() << right.Int64()), nil
	case ">>":
		return Int(left.Int64() >> right.Int64()), nil
	case "&":
		return Int(left.Int64() & right.Int64()), nil
	case "|":
		return Int(left.Int64() | right.Int64()), nil
	case "^":
		return Int(left.Int64() ^ right.Int64()), nil
	case "==":
		if left.Kind() == StringKind || right.Kind() == StringKind {
			if left.String() == right.String() {
				return Int(1), nil
			}
			return Int(0), nil
		}
		if left.Int64() == right.Int64() {
			return Int(1), nil
		}
		return Int(0), nil
	case "!=":
		if left.Kind() == StringKind || right.Kind() == StringKind {
			if left.String() != right.String() {
				return Int(1), nil
			}
			return Int(0), nil
		}
		if left.Int64() != right.Int64() {
			return Int(1), nil
		}
		return Int(0), nil
	case "<":
		if left.Int64() < right.Int64() {
			return Int(1), nil
		}
		return Int(0), nil
	case "<=":
		if left.Int64() <= right.Int64() {
			return Int(1), nil
		}
		return Int(0), nil
	case ">":
		if left.Int64() > right.Int64() {
			return Int(1), nil
		}
		return Int(0), nil
	case ">=":
		if left.Int64() >= right.Int64() {
			return Int(1), nil
		}
		return Int(0), nil
	case "&&":
		if left.Truthy() && right.Truthy() {
			return Int(1), nil
		}
		return Int(0), nil
	case "||":
		if left.Truthy() || right.Truthy() {
			return Int(1), nil
		}
		return Int(0), nil
	default:
		return Value{}, fmt.Errorf("unsupported binary operator %q", op)
	}
}

func evalAssignBinary(op string, left, right Value) (Value, error) {
	switch op {
	case "+=":
		return evalBinary("+", left, right)
	case "-=":
		return evalBinary("-", left, right)
	case "*=":
		return evalBinary("*", left, right)
	case "/=":
		return evalBinary("/", left, right)
	case "%=":
		return evalBinary("%", left, right)
	case "&=":
		return evalBinary("&", left, right)
	case "|=":
		return evalBinary("|", left, right)
	case "^=":
		return evalBinary("^", left, right)
	default:
		return Value{}, fmt.Errorf("unsupported assignment operator %q", op)
	}
}
