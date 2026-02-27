# Emuera Compatibility Status

This project targets full native compatibility with Emuera

## Command coverage snapshot

- Emuera FunctionCode keys in parser target: `268`
- parser recognition in Go: `268/268`
- runtime dispatch/handler coverage: `257/268` command-key paths
- remaining `11` are structural control/data blocks handled by dedicated AST execution:
  - `IF`, `SIF`, `WHILE`, `DO`, `FOR`, `REPEAT`, `SELECTCASE`, `STRDATA`, `RETURN`, `BREAK`, `CONTINUE`

Implemented execution entries (native runtime):
- `PRINT`, `PRINTL`
- `IF`, `ELSEIF`, `ELSE`, `ENDIF`, `SIF`
- `GOTO`
- `CALL`
- `RETURN`
- `BEGIN`
- `QUIT`
- `BREAK`, `CONTINUE`
- `REPEAT`, `REND`
- `FOR`, `NEXT`
- `WHILE`, `WEND`
- `DO`, `LOOP`
- Generic dispatcher for all remaining known commands (currently no-op or partial behavior depending on command family)
- Additional runtime support:
  - CSV command family baseline (`CSV*`)
  - Save/load command baseline (`SAVEGAME`, `LOADGAME`, `SAVEDATA`, `LOADDATA`, `DELDATA`, `CHKDATA`, `SAVEGLOBAL`, `LOADGLOBAL`)
  - PRINTFORM baseline (`%expr%`, `{expr}` placeholder evaluation)
  - Method-like command baseline (`ABS`, `SIGN`, `MAX`, `MIN`, `POWER`, `SQRT`, `CBRT`, `LOG`, `LOG10`, `EXPONENT`, `LIMIT`, `INRANGE`, `RAND`, `STRLEN*`, `STRFIND*`, `SUBSTRING*`, `TOINT`, `TOSTR`, `EXISTCSV`, `REGEXPMATCH`)
  - HTML string functions (`HTML_STRINGLEN`, `HTML_SUBSTRING`, `HTML_STRINGLINES`)
  - Dynamic variable functions (`ISDEFINED`, `EXISTVAR`, `GETVAR`, `GETVARS`, `SETVAR`)
  - Enumeration functions (`ENUMFUNC*`, `ENUMVAR*`, `ENUMMACRO*`, `EXISTFUNCTION`)
  - Color functions (`COLOR_FROMNAME`, `COLOR_FROMRGB`)
  - Character data functions (`CHKCHARADATA`, `FIND_CHARADATA`)
  - Variable/bit operation baseline (`VARSET`, `CVARSET`, `GETBIT`, `SETBIT`, `CLEARBIT`, `INVERTBIT`)
  - Block command baseline (`SELECTCASE`, `CASE`, `CASEELSE`, `ENDSELECT`, `STRDATA`, `PRINTDATA*`, `DATA`, `DATAFORM`, `ENDDATA`)
  - Indexed variable baseline (`#DIM/#DIMS` ingest, `VAR:idx` read/write in parser/runtime, save/load )
  - Scope/prefix baseline
  - Additional command families:
    - Array helpers (`ARRAYSHIFT`, `ARRAYREMOVE`, `SWAP`)
    - Character helpers baseline (`ADDCHARA*`, `DELCHARA*`, `GETCHARA`, `FINDCHARA*`, `SWAPCHARA`, `SORTCHARA`, `COPYCHARA`, `ADDCOPYCHARA`, `PICKUPCHARA`)
    - UI/state helpers baseline (`ALIGNMENT`, `CURRENTALIGN`, `REDRAW`, `CURRENTREDRAW`, `SKIPDISP`, `ISSKIP`, `SETCOLOR*`, `SETBGCOLOR*`, `GETCOLOR*`, `SETFONT/GETFONT/CHKFONT`, `FONT*`, `PRINTCPERLINE`)
    - Line helpers baseline (`DRAWLINE*`, `CLEARLINE`, `REUSELASTLINE`)

Implemented assignment operators:
- `=`
- `+=`, `-=`, `*=`, `/=`, `%=`, `&=`, `|=`, `^=`
- prefix/postfix `++`, `--`

## Remaining major areas

- Full PRINT family (`PRINTFORM*`, `PRINTDATA*`, `PRINTBUTTON*`, etc.)
- Input/wait commands (`INPUT*`, `TINPUT*`, `WAIT*`, `BINPUT*`, `ONEBINPUT*`)
- Jump/call variants (`TRY*`, `CALLF`, `JUMP*`, `GOTOFORM*`)
- CSV/character/value systems (`CSV*`, `ADDCHARA*`, `CVARSET`, etc.)
- Save/load/data commands (`SAVE*`, `LOAD*`, `SAVEDATA`, `CHKDATA`, ...)
- Color/font/layout/UI state commands
- Random/time/debug/mouse/system commands
- Select/case and additional expression/form features

## Porting strategy

1. Implement parser-level compatibility for Emuera command spellings.
2. Port runtime value model (global/static/dynamic/character) and typed arrays.
3. Port command groups by subsystem and lock with compatibility tests.
4. Add and maintain native regression test fixtures for command groups.
