package parser

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gosuda/erago/ast"
)

type ERBResult struct {
	Functions      map[string]*ast.Function
	Order          []string
	EventFunctions map[string][]*ast.Function // Event functions that should be called multiple times
}

// isEventFunction returns true if the function name is an event function
// Event functions are called in order (with #PRI first) when triggered by BEGIN
func isEventFunction(name string) bool {
	switch name {
	case "EVENTSHOP", "EVENTFIRST", "EVENTTRAIN", "EVENTEND", "EVENTTURNEND",
		"EVENTCOM", "EVENTLOAD", "SYSTEM_TITLE":
		return true
	}
	return false
}

func ParseERB(files map[string]string, macros map[string]struct{}) (*ERBResult, error) {
	result := &ERBResult{
		Functions:      map[string]*ast.Function{},
		Order:          []string{},
		EventFunctions: map[string][]*ast.Function{},
	}
	for _, file := range sortedKeys(files) {
		lines := preprocess(toLines(file, files[file]), macros)
		for i := 0; i < len(lines); {
			if !strings.HasPrefix(lines[i].Content, "@") {
				return nil, fmt.Errorf("%s:%d: expected function definition, got %q", lines[i].File, lines[i].Number, lines[i].Content)
			}
			fn, consumed, err := parseFunction(lines, i)
			if err != nil {
				return nil, err
			}
			// Event functions are tracked separately and called in order
			if isEventFunction(fn.Name) {
				result.EventFunctions[fn.Name] = append(result.EventFunctions[fn.Name], fn)
				// Also keep the first one in Functions map for lookups
				if result.Functions[fn.Name] == nil {
					result.Functions[fn.Name] = fn
					result.Order = append(result.Order, fn.Name)
				}
				i += consumed
				continue
			}
			if existing, exists := result.Functions[fn.Name]; exists {
				if err := mergeDuplicateFunction(existing, fn); err != nil {
					return nil, fmt.Errorf("%s:%d: duplicate function %s: %w", lines[i].File, lines[i].Number, fn.Name, err)
				}
				i += consumed
				continue
			}
			result.Functions[fn.Name] = fn
			result.Order = append(result.Order, fn.Name)
			i += consumed
		}
	}
	// Sort event functions by priority (higher priority first)
	for name, fns := range result.EventFunctions {
		sortEventFunctions(fns)
		result.EventFunctions[name] = fns
	}
	return result, nil
}

// sortEventFunctions sorts functions by priority (higher priority first)
func sortEventFunctions(fns []*ast.Function) {
	for i := 0; i < len(fns)-1; i++ {
		for j := i + 1; j < len(fns); j++ {
			if fns[j].Priority > fns[i].Priority {
				fns[i], fns[j] = fns[j], fns[i]
			}
		}
	}
}

func mergeDuplicateFunction(dst, src *ast.Function) error {
	if dst == nil || src == nil {
		return nil
	}
	if len(dst.Args) != len(src.Args) {
		return fmt.Errorf("argument count mismatch (%d vs %d)", len(dst.Args), len(src.Args))
	}
	for i := range dst.Args {
		if dst.Args[i].Name != src.Args[i].Name {
			return fmt.Errorf("argument mismatch at %d (%s vs %s)", i, dst.Args[i].Name, src.Args[i].Name)
		}
	}
	if dst.Body == nil {
		dst.Body = &ast.Thunk{Statements: nil, LabelMap: map[string]int{}}
	}
	if dst.Body.LabelMap == nil {
		dst.Body.LabelMap = map[string]int{}
	}
	if src.Body == nil {
		dst.VarDecls = append(dst.VarDecls, src.VarDecls...)
		return nil
	}
	offset := len(dst.Body.Statements)
	dst.Body.Statements = append(dst.Body.Statements, src.Body.Statements...)
	for name, idx := range src.Body.LabelMap {
		dst.Body.LabelMap[name] = idx + offset
	}
	dst.VarDecls = append(dst.VarDecls, src.VarDecls...)
	return nil
}

func parseFunction(lines []Line, from int) (*ast.Function, int, error) {
	def := lines[from]
	name, args, err := parseFunctionDef(def.Content)
	if err != nil {
		return nil, 0, fmt.Errorf("%s:%d: %w", def.File, def.Number, err)
	}
	idx := from + 1
	varDecls := make([]ast.VarDecl, 0, 2)
	priority := 0
	for idx < len(lines) && strings.HasPrefix(lines[idx].Content, "#") {
		prop := strings.TrimSpace(lines[idx].Content[1:])
		upper := strings.ToUpper(prop)
		if upper == "PRI" {
			priority = 1
		} else if strings.HasPrefix(upper, "DIMS ") {
			if decl, ok := parseDimDecl(prop[len("DIMS"):], true, "local"); ok {
				varDecls = append(varDecls, decl)
			}
		} else if strings.HasPrefix(upper, "DIM ") {
			if decl, ok := parseDimDecl(prop[len("DIM"):], false, "local"); ok {
				varDecls = append(varDecls, decl)
			}
		}
		idx++
	}
	end := idx
	for end < len(lines) && !strings.HasPrefix(lines[end].Content, "@") {
		end++
	}
	thunk, consumed, err := parseThunk(lines[idx:end], 0, nil)
	if err != nil {
		return nil, 0, err
	}
	if consumed != end-idx {
		return nil, 0, fmt.Errorf("%s:%d: parser consumed %d/%d lines", def.File, def.Number, consumed, end-idx)
	}
	return &ast.Function{Name: name, Args: args, Body: thunk, VarDecls: varDecls, Priority: priority}, end - from, nil
}

func parseFunctionDef(raw string) (string, []ast.Arg, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "@") {
		return "", nil, fmt.Errorf("function must start with @")
	}
	raw = strings.TrimSpace(raw[1:])
	if raw == "" {
		return "", nil, fmt.Errorf("missing function name")
	}
	var name string
	var argsRaw string
	if i := strings.Index(raw, "("); i >= 0 {
		name = strings.TrimSpace(raw[:i])
		if !strings.HasSuffix(raw, ")") {
			return "", nil, fmt.Errorf("invalid function argument list")
		}
		argsRaw = strings.TrimSpace(raw[i+1 : len(raw)-1])
	} else {
		name, argsRaw = splitNameAndRest(raw)
	}
	if name == "" {
		return "", nil, fmt.Errorf("missing function name")
	}
	args, err := parseArgs(argsRaw)
	if err != nil {
		return "", nil, err
	}
	return strings.ToUpper(name), args, nil
}

func parseArgs(raw string) ([]ast.Arg, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := splitTopLevel(raw, ',')
	args := make([]ast.Arg, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		nameRaw := strings.TrimSpace(part)
		var def ast.Expr
		if idx := strings.Index(part, "="); idx >= 0 {
			nameRaw = strings.TrimSpace(part[:idx])
			exprRaw := strings.TrimSpace(part[idx+1:])
			e, err := ParseExpr(exprRaw)
			if err != nil {
				return nil, fmt.Errorf("invalid default argument expression %q: %w", exprRaw, err)
			}
			def = e
		}
		target, err := parseVarRefText(nameRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid argument name %q: %w", nameRaw, err)
		}
		if strings.TrimSpace(target.Name) == "" {
			return nil, fmt.Errorf("invalid argument name %q", nameRaw)
		}
		target.Name = strings.ToUpper(strings.TrimSpace(target.Name))
		args = append(args, ast.Arg{Name: target.Name, Target: target, Default: def})
	}
	return args, nil
}

func parseThunk(lines []Line, from int, until func(string) bool) (*ast.Thunk, int, error) {
	stmts := make([]ast.Statement, 0, len(lines)-from)
	labels := map[string]int{}
	idx := from
	for idx < len(lines) {
		upper := strings.ToUpper(lines[idx].Content)
		if until != nil && until(upper) {
			break
		}
		if strings.HasPrefix(lines[idx].Content, "$") {
			label := strings.ToUpper(strings.TrimSpace(lines[idx].Content[1:]))
			if label == "" {
				return nil, 0, fmt.Errorf("%s:%d: empty label", lines[idx].File, lines[idx].Number)
			}
			labels[label] = len(stmts)
			idx++
			continue
		}
		stmt, consumed, err := parseStatement(lines, idx)
		if err != nil {
			return nil, 0, err
		}
		stmts = append(stmts, stmt)
		idx += consumed
	}
	return &ast.Thunk{Statements: stmts, LabelMap: labels}, idx - from, nil
}

func parseStatement(lines []Line, index int) (ast.Statement, int, error) {
	line := lines[index]
	content := strings.TrimSpace(line.Content)
	if content == "" {
		return ast.PrintStmt{Expr: ast.StringLit{Value: ""}, NewLine: true}, 1, nil
	}
	upper := strings.ToUpper(content)
	if strings.HasPrefix(upper, "IF ") || upper == "IF" {
		stmt, consumed, err := parseIf(lines, index)
		if err != nil {
			return nil, 0, err
		}
		return stmt, consumed, nil
	}
	if strings.HasPrefix(upper, "WHILE ") || upper == "WHILE" {
		stmt, consumed, err := parseWhile(lines, index)
		if err != nil {
			return nil, 0, err
		}
		return stmt, consumed, nil
	}
	if strings.HasPrefix(upper, "DO ") || upper == "DO" {
		stmt, consumed, err := parseDoWhile(lines, index)
		if err != nil {
			return nil, 0, err
		}
		return stmt, consumed, nil
	}
	if strings.HasPrefix(upper, "REPEAT ") || upper == "REPEAT" {
		stmt, consumed, err := parseRepeat(lines, index)
		if err != nil {
			return nil, 0, err
		}
		return stmt, consumed, nil
	}
	if strings.HasPrefix(upper, "FOR ") || upper == "FOR" {
		stmt, consumed, err := parseFor(lines, index)
		if err != nil {
			return nil, 0, err
		}
		return stmt, consumed, nil
	}
	cmd, rest := splitNameAndRest(content)
	if !isKnownCommand(strings.ToUpper(cmd)) {
		if knownCmd, knownRest, ok := splitKnownCommandPrefix(content); ok {
			cmd, rest = knownCmd, knownRest
		}
	}
	switch strings.ToUpper(cmd) {
	case "PRINT":
		e, err := parsePrintExpr(rest)
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d: %w", line.File, line.Number, err)
		}
		return ast.PrintStmt{Expr: e, NewLine: false}, 1, nil
	case "PRINTL":
		e, err := parsePrintExpr(rest)
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d: %w", line.File, line.Number, err)
		}
		return ast.PrintStmt{Expr: e, NewLine: true}, 1, nil
	case "GOTO":
		label := strings.ToUpper(strings.TrimSpace(rest))
		if label == "" {
			return nil, 0, fmt.Errorf("%s:%d: missing goto label", line.File, line.Number)
		}
		return ast.GotoStmt{Label: label}, 1, nil
	case "CALL":
		name, args, err := parseCall(rest)
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d: %w", line.File, line.Number, err)
		}
		return ast.CallStmt{Name: name, Args: args}, 1, nil
	case "RETURN":
		args, err := ParseExprList(rest)
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d: %w", line.File, line.Number, err)
		}
		return ast.ReturnStmt{Values: args}, 1, nil
	case "BEGIN":
		kw := strings.ToUpper(strings.TrimSpace(rest))
		if kw == "" {
			return nil, 0, fmt.Errorf("%s:%d: missing BEGIN keyword", line.File, line.Number)
		}
		return ast.BeginStmt{Keyword: kw}, 1, nil
	case "QUIT":
		return ast.QuitStmt{}, 1, nil
	case "BREAK":
		return ast.BreakStmt{}, 1, nil
	case "CONTINUE":
		return ast.ContinueStmt{}, 1, nil
	case "SIF":
		cond, err := ParseExpr(rest)
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d: invalid SIF condition: %w", line.File, line.Number, err)
		}
		nextIdx := index + 1
		if nextIdx >= len(lines) {
			return nil, 0, fmt.Errorf("%s:%d: SIF expects next statement", line.File, line.Number)
		}
		nextStmt, consumed, err := parseStatement(lines, nextIdx)
		if err != nil {
			return nil, 0, err
		}
		body := &ast.Thunk{
			Statements: []ast.Statement{nextStmt},
			LabelMap:   map[string]int{},
		}
		return ast.IfStmt{
			Branches: []ast.IfBranch{{Cond: cond, Body: body}},
			Else:     &ast.Thunk{Statements: nil, LabelMap: map[string]int{}},
		}, consumed + 1, nil
	case "SELECTCASE":
		stmt, consumed, err := parseSelectCase(lines, index, rest)
		if err != nil {
			return nil, 0, err
		}
		return stmt, consumed, nil
	case "STRDATA":
		stmt, consumed, err := parseStrData(lines, index, rest)
		if err != nil {
			return nil, 0, err
		}
		return stmt, consumed, nil
	}

	if strings.HasPrefix(strings.ToUpper(cmd), "PRINTDATA") {
		stmt, consumed, err := parsePrintData(lines, index, strings.ToUpper(cmd))
		if err != nil {
			return nil, 0, err
		}
		return stmt, consumed, nil
	}

	if isKnownCommand(strings.ToUpper(cmd)) {
		printNewLine, printWait := parsePrintFlags(strings.ToUpper(cmd))
		return ast.CommandStmt{
			Name:         strings.ToUpper(cmd),
			Arg:          strings.TrimSpace(rest),
			PrintNewLine: printNewLine,
			PrintWait:    printWait,
		}, 1, nil
	}

	if inc := splitIncDec(content); inc != nil {
		target, err := parseVarRefText(inc.Name)
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d: %w", line.File, line.Number, err)
		}
		return ast.IncDecStmt{Target: target, Op: inc.Op, Pre: inc.Pre}, 1, nil
	}

	if assign := splitAssign(content); assign != nil {
		if assign.Op == "'=" {
			assign.Op = "="
		}
		var e ast.Expr
		if strings.TrimSpace(assign.Right) == "" {
			e = ast.EmptyLit{}
		} else {
			parsed, err := ParseExpr(assign.Right)
			if err != nil {
				e = ast.StringLit{Value: decodeCharSeq(strings.TrimSpace(assign.Right))}
			} else {
				e = parsed
			}
		}
		target, err := parseVarRefText(assign.Left)
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d: %w", line.File, line.Number, err)
		}
		return ast.AssignStmt{Target: target, Op: assign.Op, Expr: e}, 1, nil
	}

	if name, args, ok, err := parseBareCall(content); ok {
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d: %w", line.File, line.Number, err)
		}
		return ast.CallStmt{Name: name, Args: args}, 1, nil
	}

	if isIdentifier(strings.ToUpper(cmd)) {
		printNewLine, printWait := parsePrintFlags(strings.ToUpper(cmd))
		return ast.CommandStmt{
			Name:         strings.ToUpper(cmd),
			Arg:          strings.TrimSpace(rest),
			PrintNewLine: printNewLine,
			PrintWait:    printWait,
		}, 1, nil
	}

	return nil, 0, fmt.Errorf("%s:%d: unsupported statement %q", line.File, line.Number, content)
}

func splitKnownCommandPrefix(raw string) (string, string, bool) {
	upper := strings.ToUpper(strings.TrimSpace(raw))
	best := ""
	for cmd := range knownCommands {
		if !strings.HasPrefix(upper, cmd) {
			continue
		}
		if len(upper) == len(cmd) {
			if len(cmd) > len(best) {
				best = cmd
			}
			continue
		}
		rest := raw[len(cmd):]
		r, _ := utf8.DecodeRuneInString(rest)
		if r == utf8.RuneError && len(rest) > 0 {
			continue
		}
		if !isIdentPart(r) {
			if len(cmd) > len(best) {
				best = cmd
			}
		}
	}
	if best == "" {
		return "", "", false
	}
	return best, strings.TrimSpace(raw[len(best):]), true
}

func parseWhile(lines []Line, from int) (ast.Statement, int, error) {
	line := lines[from]
	condRaw := strings.TrimSpace(line.Content[len("WHILE"):])
	cond, err := ParseExpr(condRaw)
	if err != nil {
		return nil, 0, fmt.Errorf("%s:%d: invalid WHILE condition: %w", line.File, line.Number, err)
	}
	thunk, consumed, err := parseThunk(lines, from+1, func(s string) bool { return s == "WEND" })
	if err != nil {
		return nil, 0, err
	}
	end := from + 1 + consumed
	if end >= len(lines) || strings.ToUpper(lines[end].Content) != "WEND" {
		return nil, 0, fmt.Errorf("%s:%d: WHILE without WEND", line.File, line.Number)
	}
	return ast.WhileStmt{Cond: cond, Body: thunk}, consumed + 2, nil
}

func parseDoWhile(lines []Line, from int) (ast.Statement, int, error) {
	line := lines[from]
	thunk, consumed, err := parseThunk(lines, from+1, func(s string) bool {
		return strings.HasPrefix(s, "LOOP")
	})
	if err != nil {
		return nil, 0, err
	}
	end := from + 1 + consumed
	if end >= len(lines) {
		return nil, 0, fmt.Errorf("%s:%d: DO without LOOP", line.File, line.Number)
	}
	loopLine := strings.TrimSpace(lines[end].Content)
	upper := strings.ToUpper(loopLine)
	if !strings.HasPrefix(upper, "LOOP") {
		return nil, 0, fmt.Errorf("%s:%d: DO without LOOP", line.File, line.Number)
	}
	condRaw := strings.TrimSpace(loopLine[len("LOOP"):])
	cond, err := ParseExpr(condRaw)
	if err != nil {
		return nil, 0, fmt.Errorf("%s:%d: invalid LOOP condition: %w", lines[end].File, lines[end].Number, err)
	}
	return ast.DoWhileStmt{Body: thunk, Cond: cond}, consumed + 2, nil
}

func parseRepeat(lines []Line, from int) (ast.Statement, int, error) {
	line := lines[from]
	countRaw := strings.TrimSpace(line.Content[len("REPEAT"):])
	count, err := ParseExpr(countRaw)
	if err != nil {
		return nil, 0, fmt.Errorf("%s:%d: invalid REPEAT count: %w", line.File, line.Number, err)
	}
	thunk, consumed, err := parseThunk(lines, from+1, func(s string) bool { return s == "REND" })
	if err != nil {
		return nil, 0, err
	}
	end := from + 1 + consumed
	if end >= len(lines) || strings.ToUpper(lines[end].Content) != "REND" {
		return nil, 0, fmt.Errorf("%s:%d: REPEAT without REND", line.File, line.Number)
	}
	return ast.RepeatStmt{Count: count, Body: thunk}, consumed + 2, nil
}

func parseFor(lines []Line, from int) (ast.Statement, int, error) {
	line := lines[from]
	rest := strings.TrimSpace(line.Content[len("FOR"):])
	parts := splitTopLevel(rest, ',')
	if len(parts) < 3 || len(parts) > 4 {
		return nil, 0, fmt.Errorf("%s:%d: FOR requires 3 or 4 arguments", line.File, line.Number)
	}
	target, err := parseVarRefText(parts[0])
	if err != nil {
		return nil, 0, fmt.Errorf("%s:%d: invalid FOR variable %q", line.File, line.Number, strings.TrimSpace(parts[0]))
	}
	target.Name = strings.ToUpper(strings.TrimSpace(target.Name))
	if target.Name == "" {
		return nil, 0, fmt.Errorf("%s:%d: invalid FOR variable %q", line.File, line.Number, strings.TrimSpace(parts[0]))
	}
	initExpr, err := ParseExpr(parts[1])
	if err != nil {
		return nil, 0, fmt.Errorf("%s:%d: invalid FOR init expression: %w", line.File, line.Number, err)
	}
	limitExpr, err := ParseExpr(parts[2])
	if err != nil {
		return nil, 0, fmt.Errorf("%s:%d: invalid FOR limit expression: %w", line.File, line.Number, err)
	}
	stepExpr := ast.Expr(ast.IntLit{Value: 1})
	if len(parts) == 4 {
		stepExpr, err = ParseExpr(parts[3])
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d: invalid FOR step expression: %w", line.File, line.Number, err)
		}
	}

	thunk, consumed, err := parseThunk(lines, from+1, func(s string) bool { return s == "NEXT" })
	if err != nil {
		return nil, 0, err
	}
	end := from + 1 + consumed
	if end >= len(lines) || strings.ToUpper(lines[end].Content) != "NEXT" {
		return nil, 0, fmt.Errorf("%s:%d: FOR without NEXT", line.File, line.Number)
	}
	return ast.ForStmt{
		Var:    target.Name,
		Target: target,
		Init:   initExpr,
		Limit:  limitExpr,
		Step:   stepExpr,
		Body:   thunk,
	}, consumed + 2, nil
}

func parseSelectCase(lines []Line, from int, rest string) (ast.Statement, int, error) {
	head := lines[from]
	target, err := ParseExpr(strings.TrimSpace(rest))
	if err != nil {
		return nil, 0, fmt.Errorf("%s:%d: invalid SELECTCASE expression: %w", head.File, head.Number, err)
	}

	idx := from + 1
	branches := make([]ast.SelectCaseBranch, 0, 2)
	elseThunk := &ast.Thunk{Statements: nil, LabelMap: map[string]int{}}
	for {
		if idx >= len(lines) {
			return nil, 0, fmt.Errorf("%s:%d: SELECTCASE without ENDSELECT", head.File, head.Number)
		}
		line := lines[idx]
		upper := strings.ToUpper(strings.TrimSpace(line.Content))
		switch {
		case strings.HasPrefix(upper, "CASE "):
			condRaw := strings.TrimSpace(line.Content[len("CASE"):])
			conds, err := parseCaseConditions(condRaw)
			if err != nil {
				return nil, 0, fmt.Errorf("%s:%d: %w", line.File, line.Number, err)
			}
			idx++
			body, consumed, err := parseThunk(lines, idx, func(s string) bool {
				return strings.HasPrefix(s, "CASE ") || s == "CASEELSE" || s == "ENDSELECT"
			})
			if err != nil {
				return nil, 0, err
			}
			idx += consumed
			branches = append(branches, ast.SelectCaseBranch{
				Conditions: conds,
				Body:       body,
			})
		case upper == "CASEELSE":
			idx++
			body, consumed, err := parseThunk(lines, idx, func(s string) bool { return s == "ENDSELECT" })
			if err != nil {
				return nil, 0, err
			}
			idx += consumed
			elseThunk = body
		case upper == "ENDSELECT":
			return ast.SelectCaseStmt{
				Target:   target,
				Branches: branches,
				Else:     elseThunk,
			}, idx - from + 1, nil
		default:
			return nil, 0, fmt.Errorf("%s:%d: unexpected token in SELECTCASE block: %q", line.File, line.Number, line.Content)
		}
	}
}

func parseCaseConditions(raw string) ([]ast.CaseCondition, error) {
	parts := splitTopLevel(raw, ',')
	if len(parts) == 0 {
		return nil, fmt.Errorf("CASE requires at least one condition")
	}
	conditions := make([]ast.CaseCondition, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		upper := strings.ToUpper(p)
		if strings.HasPrefix(upper, "IS ") {
			rest := strings.TrimSpace(p[len("IS"):])
			op := ""
			for _, candidate := range []string{"==", "!=", "<=", ">=", "<", ">"} {
				if strings.HasPrefix(strings.TrimSpace(rest), candidate) {
					op = candidate
					rest = strings.TrimSpace(rest[len(candidate):])
					break
				}
			}
			if op == "" {
				return nil, fmt.Errorf("invalid CASE IS comparator")
			}
			expr, err := ParseExpr(rest)
			if err != nil {
				return nil, fmt.Errorf("invalid CASE IS expression: %w", err)
			}
			conditions = append(conditions, ast.CaseCondition{
				Kind: "compare",
				Op:   op,
				Expr: expr,
			})
			continue
		}

		if i := indexWord(upper, " TO "); i >= 0 {
			fromRaw := strings.TrimSpace(p[:i])
			toRaw := strings.TrimSpace(p[i+len(" TO "):])
			fromExpr, err := ParseExpr(fromRaw)
			if err != nil {
				return nil, fmt.Errorf("invalid CASE range from expression: %w", err)
			}
			toExpr, err := ParseExpr(toRaw)
			if err != nil {
				return nil, fmt.Errorf("invalid CASE range to expression: %w", err)
			}
			conditions = append(conditions, ast.CaseCondition{
				Kind: "range",
				From: fromExpr,
				To:   toExpr,
			})
			continue
		}

		expr, err := ParseExpr(p)
		if err != nil {
			return nil, fmt.Errorf("invalid CASE condition expression: %w", err)
		}
		conditions = append(conditions, ast.CaseCondition{
			Kind: "equal",
			Expr: expr,
		})
	}
	if len(conditions) == 0 {
		return nil, fmt.Errorf("CASE requires at least one condition")
	}
	return conditions, nil
}

func parseStrData(lines []Line, from int, rest string) (ast.Statement, int, error) {
	head := lines[from]
	targetRaw := strings.TrimSpace(rest)
	if targetRaw == "" {
		return nil, 0, fmt.Errorf("%s:%d: STRDATA requires destination variable", head.File, head.Number)
	}
	target, err := parseVarRefText(targetRaw)
	if err != nil {
		return nil, 0, fmt.Errorf("%s:%d: %w", head.File, head.Number, err)
	}
	items, consumed, err := parseDataBlock(lines, from+1)
	if err != nil {
		return nil, 0, err
	}
	return ast.StrDataStmt{Target: target, Items: items}, consumed + 1, nil
}

func parsePrintData(lines []Line, from int, command string) (ast.Statement, int, error) {
	items, consumed, err := parseDataBlock(lines, from+1)
	if err != nil {
		return nil, 0, err
	}
	printNewLine, printWait := parsePrintFlags(command)
	return ast.PrintDataStmt{
		Command:      command,
		Items:        items,
		PrintNewLine: printNewLine,
		PrintWait:    printWait,
	}, consumed + 1, nil
}

func parseDataBlock(lines []Line, from int) ([]ast.DataItem, int, error) {
	idx := from
	items := make([]ast.DataItem, 0, 4)
	for {
		if idx >= len(lines) {
			return nil, 0, fmt.Errorf("%s:%d: DATA block without ENDDATA", lines[from-1].File, lines[from-1].Number)
		}
		line := lines[idx]
		upper := strings.ToUpper(strings.TrimSpace(line.Content))
		switch {
		case strings.HasPrefix(upper, "DATAFORM "):
			items = append(items, ast.DataItem{
				Kind: "dataform",
				Raw:  strings.TrimSpace(line.Content[len("DATAFORM"):]),
			})
		case upper == "DATAFORM":
			items = append(items, ast.DataItem{
				Kind: "dataform",
				Raw:  "",
			})
		case strings.HasPrefix(upper, "DATA "):
			items = append(items, ast.DataItem{
				Kind: "data",
				Raw:  strings.TrimSpace(line.Content[len("DATA"):]),
			})
		case upper == "DATA":
			items = append(items, ast.DataItem{
				Kind: "data",
				Raw:  "",
			})
		case upper == "DATALIST" || upper == "ENDLIST":
		case upper == "ENDDATA":
			return items, idx - from + 1, nil
		default:
			return nil, 0, fmt.Errorf("%s:%d: invalid token in DATA block: %q", line.File, line.Number, line.Content)
		}
		idx++
	}
}

func indexWord(s, needle string) int {
	return strings.Index(strings.ToUpper(s), strings.ToUpper(needle))
}

func parsePrintFlags(name string) (printNewLine bool, printWait bool) {
	name = strings.ToUpper(strings.TrimSpace(name))
	if !(strings.HasPrefix(name, "PRINT") || strings.HasPrefix(name, "DEBUGPRINT")) {
		return false, false
	}
	if strings.HasSuffix(name, "W") {
		return true, true
	}
	if strings.HasPrefix(name, "PRINTL") || strings.HasPrefix(name, "DEBUGPRINTL") || strings.HasSuffix(name, "L") {
		return true, false
	}
	return false, false
}

func parseIf(lines []Line, from int) (ast.Statement, int, error) {
	idx := from
	branches := make([]ast.IfBranch, 0, 2)
	elseThunk := &ast.Thunk{Statements: nil, LabelMap: map[string]int{}}
	for {
		if idx >= len(lines) {
			return nil, 0, fmt.Errorf("%s:%d: unterminated IF block", lines[from].File, lines[from].Number)
		}
		line := lines[idx]
		upper := strings.ToUpper(line.Content)
		switch {
		case strings.HasPrefix(upper, "IF ") || upper == "IF":
			condRaw := strings.TrimSpace(line.Content[len("IF"):])
			cond, err := ParseExpr(condRaw)
			if err != nil {
				return nil, 0, fmt.Errorf("%s:%d: invalid IF condition: %w", line.File, line.Number, err)
			}
			idx++
			thunk, consumed, err := parseThunk(lines, idx, func(s string) bool {
				return strings.HasPrefix(s, "ELSEIF ") || s == "ELSE" || s == "ENDIF"
			})
			if err != nil {
				return nil, 0, err
			}
			idx += consumed
			branches = append(branches, ast.IfBranch{Cond: cond, Body: thunk})
		case strings.HasPrefix(upper, "ELSEIF "):
			condRaw := strings.TrimSpace(line.Content[len("ELSEIF"):])
			cond, err := ParseExpr(condRaw)
			if err != nil {
				return nil, 0, fmt.Errorf("%s:%d: invalid ELSEIF condition: %w", line.File, line.Number, err)
			}
			idx++
			thunk, consumed, err := parseThunk(lines, idx, func(s string) bool {
				return strings.HasPrefix(s, "ELSEIF ") || s == "ELSE" || s == "ENDIF"
			})
			if err != nil {
				return nil, 0, err
			}
			idx += consumed
			branches = append(branches, ast.IfBranch{Cond: cond, Body: thunk})
		case upper == "ELSE":
			idx++
			thunk, consumed, err := parseThunk(lines, idx, func(s string) bool { return s == "ENDIF" })
			if err != nil {
				return nil, 0, err
			}
			idx += consumed
			elseThunk = thunk
		case upper == "ENDIF":
			if len(branches) == 0 {
				return nil, 0, fmt.Errorf("%s:%d: empty IF block", lines[from].File, lines[from].Number)
			}
			return ast.IfStmt{Branches: branches, Else: elseThunk}, idx - from + 1, nil
		default:
			return nil, 0, fmt.Errorf("%s:%d: invalid token inside IF block: %q", line.File, line.Number, line.Content)
		}
	}
}

func parsePrintExpr(raw string) (ast.Expr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ast.StringLit{Value: ""}, nil
	}
	return ast.StringLit{Value: decodeCharSeq(raw)}, nil
}

func parseCall(raw string) (string, []ast.Expr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, fmt.Errorf("missing call target")
	}
	if i := strings.Index(raw, "("); i >= 0 {
		comma := strings.Index(raw, ",")
		if comma >= 0 && comma < i {
			i = -1
		}
		if i >= 0 {
			if !strings.HasSuffix(raw, ")") {
				return "", nil, fmt.Errorf("invalid call syntax")
			}
			name := strings.ToUpper(strings.TrimSpace(raw[:i]))
			argRaw := strings.TrimSpace(raw[i+1 : len(raw)-1])
			if !isIdentifier(name) {
				return "", nil, fmt.Errorf("invalid function name %q", name)
			}
			args, err := ParseExprList(argRaw)
			if err != nil {
				parts := splitTopLevel(argRaw, ',')
				args = make([]ast.Expr, 0, len(parts))
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p == "" {
						args = append(args, ast.EmptyLit{})
						continue
					}
					args = append(args, ast.StringLit{Value: decodeCharSeq(p)})
				}
			}
			return name, args, nil
		}
	}
	parts := splitTopLevel(raw, ',')
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("missing call target")
	}
	name := strings.ToUpper(strings.TrimSpace(parts[0]))
	if !isIdentifier(name) {
		return "", nil, fmt.Errorf("invalid function name %q", name)
	}
	args := make([]ast.Expr, 0, len(parts)-1)
	for _, p := range parts[1:] {
		p = strings.TrimSpace(p)
		if p == "" {
			args = append(args, ast.EmptyLit{})
			continue
		}
		e, err := ParseExpr(p)
		if err != nil {
			args = append(args, ast.StringLit{Value: decodeCharSeq(p)})
			continue
		}
		args = append(args, e)
	}
	return name, args, nil
}

func parseBareCall(raw string) (string, []ast.Expr, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, false, nil
	}
	if strings.Contains(raw, " ") {
		return "", nil, false, nil
	}
	if i := strings.Index(raw, "("); i > 0 && strings.HasSuffix(raw, ")") {
		name := strings.ToUpper(strings.TrimSpace(raw[:i]))
		if !isIdentifier(name) {
			return "", nil, false, nil
		}
		args, err := ParseExprList(strings.TrimSpace(raw[i+1 : len(raw)-1]))
		if err != nil {
			return "", nil, true, err
		}
		return name, args, true, nil
	}
	return "", nil, false, nil
}

func parseVarRefText(raw string) (ast.VarRef, error) {
	trimmed := strings.TrimSpace(raw)
	expr, err := ParseExpr(trimmed)
	if err != nil {
		if i := strings.IndexRune(trimmed, ':'); i > 0 {
			rewritten := strings.TrimSpace(trimmed[:i]) + ":(" + strings.TrimSpace(trimmed[i+1:]) + ")"
			expr2, err2 := ParseExpr(rewritten)
			if err2 == nil {
				if ref2, ok := expr2.(ast.VarRef); ok {
					return ref2, nil
				}
			}
		}
		return ast.VarRef{}, err
	}
	ref, ok := expr.(ast.VarRef)
	if !ok {
		if i := strings.IndexRune(trimmed, ':'); i > 0 {
			rewritten := strings.TrimSpace(trimmed[:i]) + ":(" + strings.TrimSpace(trimmed[i+1:]) + ")"
			expr2, err2 := ParseExpr(rewritten)
			if err2 == nil {
				if ref2, ok2 := expr2.(ast.VarRef); ok2 {
					return ref2, nil
				}
			}
		}
		return ast.VarRef{}, fmt.Errorf("invalid variable target: %q", raw)
	}
	return ref, nil
}

type assignParts struct {
	Left  string
	Op    string
	Right string
}

func splitAssign(raw string) *assignParts {
	depth := 0
	inStr := false
	escape := false
	runes := []rune(raw)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
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
		case '=':
			if depth != 0 {
				continue
			}
			prev := rune(0)
			next := rune(0)
			prev2 := rune(0)
			if i > 0 {
				prev = runes[i-1]
			}
			if i > 1 {
				prev2 = runes[i-2]
			}
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			if next == '=' || prev == '!' || prev == '=' {
				continue
			}
			if (prev == '<' || prev == '>') && prev2 != prev {
				continue
			}
			left := strings.TrimSpace(string(runes[:i]))
			right := strings.TrimSpace(string(runes[i+1:]))
			if left == "" {
				return nil
			}
			op := "="
			if strings.HasSuffix(left, "'") {
				op = "'="
				left = strings.TrimSpace(left[:len(left)-1])
			} else {
				for _, c := range []string{"<<", ">>", "+", "-", "*", "/", "%", "&", "|", "^"} {
					if strings.HasSuffix(left, c) {
						op = c + "="
						left = strings.TrimSpace(left[:len(left)-len(c)])
						break
					}
				}
			}
			return &assignParts{Left: left, Op: op, Right: right}
		}
	}
	return nil
}

type incDecParts struct {
	Name string
	Op   string
	Pre  bool
}

func splitIncDec(raw string) *incDecParts {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) < 3 {
		return nil
	}
	if strings.HasPrefix(trimmed, "++") || strings.HasPrefix(trimmed, "--") {
		op := trimmed[:2]
		name := strings.TrimSpace(trimmed[2:])
		if name == "" {
			return nil
		}
		return &incDecParts{Name: name, Op: op, Pre: true}
	}
	if strings.HasSuffix(trimmed, "++") || strings.HasSuffix(trimmed, "--") {
		op := trimmed[len(trimmed)-2:]
		name := strings.TrimSpace(trimmed[:len(trimmed)-2])
		if name == "" {
			return nil
		}
		return &incDecParts{Name: name, Op: op, Pre: false}
	}
	return nil
}

func isIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}
