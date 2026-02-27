# erago

Go implementation scaffold of an `emuera`-style runtime, designed with the same high-level flow as [eraJS](https://github.com/undercrow/eraJS):

1. compile scripts (`ERH` + `ERB`)
2. build VM
3. run entry function and process flow control (`BEGIN`, `CALL`, `GOTO`, `RETURN`, `QUIT`)

## Current scope

This is a minimal executable core, not full emuera compatibility.

Supported now (native Go):
- Function parsing: `@FUNC(...)`
- Labels: `$LABEL`
- Statements:
  - Output: `PRINT`, `PRINTL`
  - Assignment: `=`, `+=`, `-=`, `*=`, `/=`, `%=`, `&=`, `|=`, `^=`, `++`, `--`
  - Control flow: `IF/ELSEIF/ELSE/ENDIF`, `SIF`, `GOTO`, `BREAK`, `CONTINUE`
  - Loop: `FOR/NEXT`, `WHILE/WEND`, `DO/LOOP`, `REPEAT/REND`
  - Call/flow: `CALL`, `RETURN`, `BEGIN`, `QUIT`
- Expression evaluator: integers, strings, variables, unary (`+ - ! ~`), binary operators (`* / % + - << >> < <= > >= == != & | ^ && ||`)
- ERH preprocessor basics: comment stripping, macro blocks (`[IF ...]...[ENDIF]`), `#DEFINE`
- eraJS command keyword recognition: `268/268` parser keys (runtime behavior is still being filled subsystem by subsystem)
- Runtime additions in this batch:
  - `PRINTFORM*` placeholder expansion (`%expr%`, `{expr}` basic support)
  - CSV lookup command baseline (`CSV*` command family -> `RESULT`)
  - Save/load baseline (`SAVEGAME`, `LOADGAME`, `SAVEDATA`, `LOADDATA`, `DELDATA`, `CHKDATA`, `SAVEGLOBAL`, `LOADGLOBAL`)
  - Method-like command baseline (`ABS`, `SIGN`, `MAX`, `MIN`, `POWER`, `SQRT`, `LIMIT`, `INRANGE`, `RAND`, `STRLEN*`, `STRFIND*`, `SUBSTRING*`, `TOINT`, `TOSTR`, `EXISTCSV`)
  - Variable/bit command baseline (`VARSET`, `CVARSET`, `GETBIT`, `SETBIT`, `CLEARBIT`, `INVERTBIT`)
  - Block command baseline (`SELECTCASE/CASE/CASEELSE/ENDSELECT`, `STRDATA`, `PRINTDATA*`, `DATA`, `DATAFORM`, `ENDDATA`)
  - Indexed variable baseline:
    - `#DIM/#DIMS` declaration ingestion
    - `VAR:idx` access in expressions/assignment
    - array data persisted in save/load snapshots
  - Scope/prefix baseline:
    - function property `#DIM/#DIMS` -> local declarations
    - `DYNAMIC` arrays auto-grow on indexed write
    - `REF` declarations with reference binding (`R = A` 형태)
  - Additional command baseline:
    - array helpers (`ARRAYSHIFT`, `ARRAYREMOVE`, `SWAP`)
    - character helpers (`ADDCHARA*`, `DELCHARA*`, `GETCHARA`, `FINDCHARA*`, `SWAPCHARA`, `SORTCHARA`, `COPYCHARA`, `ADDCOPYCHARA`, `PICKUPCHARA`)
    - UI helpers (`ALIGNMENT`, `CURRENTALIGN`, `REDRAW`, `CURRENTREDRAW`, `SKIPDISP`, `ISSKIP`, `SETCOLOR*`, `SETBGCOLOR*`, `GETCOLOR*`, `SETFONT/GETFONT/CHKFONT`, `FONT*`)

## Run

```bash
go run ./cmd/erago -dir ./examples/basic -entry TITLE
```

## Library usage

```go
vm, err := erago.Compile(files)
if err != nil {
    panic(err)
}

outputs, err := vm.Run("TITLE")
if err != nil {
    panic(err)
}
```

## Next work for fuller compatibility

- Full emuera command surface (many `PRINT*`, I/O, save/load, character/csv systems)
- Inline function calls in expressions
- Array/scoped variables and property directives (`#DIM`, `#LOCALSIZE`, ...)
- Rich output model (wait/input/chunk/button) compatible with UI frontend
