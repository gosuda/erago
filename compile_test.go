package erago_test

import (
	"testing"

	"github.com/gosuda/erago"
	"github.com/gosuda/erago/parser"
)

func TestCompileAndRunBasicFlow(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
A = 10
CALL HELLO(A)
IF RESULT == 11
    PRINTL ok
ELSE
    PRINTL ng
ENDIF
BEGIN NEXT

@HELLO(X)
RETURN X + 1

@NEXT
PRINTL done
QUIT
`,
	}

	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if len(out) != 2 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if out[0].Text != "ok" || !out[0].NewLine {
		t.Fatalf("unexpected first output: %+v", out[0])
	}
	if out[1].Text != "done" || !out[1].NewLine {
		t.Fatalf("unexpected second output: %+v", out[1])
	}
}

func TestGotoLabel(t *testing.T) {
	files := map[string]string{
		"LOOP.ERB": `
@TITLE
A = 0
$LOOP
A = A + 1
IF A < 3
    GOTO LOOP
ENDIF
PRINTVL A
QUIT
`,
	}

	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if len(out) != 1 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if out[0].Text != "3" {
		t.Fatalf("unexpected output text: %q", out[0].Text)
	}
}

func TestLoopAndAssignOperators(t *testing.T) {
	files := map[string]string{
		"CTRL.ERB": `
@TITLE
A = 0
FOR I, 1, 3
    A += I
NEXT

REPEAT 2
    A += 10
REND

WHILE A < 30
    A++
WEND

DO
    A -= 3
LOOP A > 25

PRINTVL A
QUIT
`,
	}

	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if len(out) != 1 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	// 0 + (1+2+3) + 20 => 26, WHILE makes 30, DO/LOOP subtracts while >25 => 24
	if out[0].Text != "24" {
		t.Fatalf("unexpected output text: %q", out[0].Text)
	}
}

func TestKnownCommandCoverageAndGenericDispatch(t *testing.T) {
	if parser.KnownCommandCount() < 268 {
		t.Fatalf("expected at least 268 known commands, got %d", parser.KnownCommandCount())
	}

	files := map[string]string{
		"GENERIC.ERB": `
@TITLE
WAIT
INITRAND 123
TRYCALL MISSING_FN
PRINTFORML ok
QUIT
`,
	}

	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	vm.SetSaveDir(t.TempDir())
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if out[0].Text != "ok" || !out[0].NewLine {
		t.Fatalf("unexpected output: %+v", out[0])
	}
}

func TestCSVAndPrintForm(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
A = 5
CSVABL 1
PRINTFORML name=%RESULT%, a={A+1}
QUIT
`,
		"ABL.CSV": "1,Strength\n2,Stamina\n",
	}

	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if out[0].Text != "name=Strength, a=6" {
		t.Fatalf("unexpected printform output: %q", out[0].Text)
	}
}

func TestSaveLoadCommands(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
A = 42
SAVEGAME 777
A = 0
LOADGAME 777
PRINTVL A
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 1 || out[0].Text != "42" {
		t.Fatalf("unexpected save/load behavior: %+v", out)
	}
}

func TestVarSetBitAndSubstring(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
VARSET A, 1
SETBIT A, 3
GETBIT A, 3
PRINTVL RESULT
SUBSTRING "abcdef", 2, 3
PRINTVL RESULTS
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if out[0].Text != "1" {
		t.Fatalf("unexpected bit output: %q", out[0].Text)
	}
	if out[1].Text != "cde" {
		t.Fatalf("unexpected substring output: %q", out[1].Text)
	}
}

func TestSelectCaseFlow(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
A = 7
SELECTCASE A
CASE 1
    PRINTL one
CASE 3 TO 8
    PRINTL range
CASEELSE
    PRINTL else
ENDSELECT
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 1 || out[0].Text != "range" {
		t.Fatalf("unexpected selectcase output: %+v", out)
	}
}

func TestStrDataAndPrintDataBlock(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
STRDATA S
    DATA alpha
    DATA beta
ENDDATA
PRINTVL S

PRINTDATAL
    DATAFORM hello %1+1%
ENDDATA
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if out[1].Text != "hello 2" {
		t.Fatalf("unexpected printdata output: %q", out[1].Text)
	}
}

func TestDimAndIndexedVariables(t *testing.T) {
	files := map[string]string{
		"MAIN.ERH": `
#DIM ARR, 5
#DIMS SARR, 4
`,
		"MAIN.ERB": `
@TITLE
ARR:1 = 7
ARR:2 = ARR:1 + 3
SARR:1 = "abc"
PRINTVL ARR:2
PRINTVL SARR:1
QUIT
`,
	}

	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if out[0].Text != "10" || out[1].Text != "abc" {
		t.Fatalf("unexpected indexed var outputs: %+v", out)
	}
}

func TestSaveLoadIncludesArrays(t *testing.T) {
	files := map[string]string{
		"MAIN.ERH": `
#DIM ARR, 3
`,
		"MAIN.ERB": `
@TITLE
ARR:1 = 55
SAVEGAME 991
ARR:1 = 1
LOADGAME 991
PRINTVL ARR:1
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	vm.SetSaveDir(t.TempDir())
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 1 || out[0].Text != "55" {
		t.Fatalf("unexpected array save/load behavior: %+v", out)
	}
}

func TestLocalDynamicRefAndVarSetIndex(t *testing.T) {
	files := map[string]string{
		"MAIN.ERH": `
#DIM DYNAMIC GARR, 1
#DIM REF R
#DIMS SARR, 3
`,
		"MAIN.ERB": `
@TITLE
GARR:5 = 7
VARSET SARR:1, "hi"
A = 4
R = A
A = 9
PRINTVL R
R = 3
PRINTVL A
CALL F
PRINTVL X
PRINTVL GARR:5
PRINTVL SARR:1
QUIT

@F
#DIM LOCAL X, 2
X:1 = 99
PRINTVL X:1
RETURN
`,
	}

	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 6 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if out[0].Text != "9" || out[1].Text != "3" {
		t.Fatalf("unexpected ref outputs: %+v", out[:2])
	}
	if out[2].Text != "99" || out[3].Text != "0" {
		t.Fatalf("unexpected local scope outputs: %+v", out[2:4])
	}
	if out[4].Text != "7" || out[5].Text != "hi" {
		t.Fatalf("unexpected dynamic/index outputs: %+v", out[4:6])
	}
}

func TestArrayShiftRemoveSwap(t *testing.T) {
	files := map[string]string{
		"MAIN.ERH": `
#DIM ARR, 6
`,
		"MAIN.ERB": `
@TITLE
ARR:0 = 10
ARR:1 = 20
ARR:2 = 30
ARRAYSHIFT ARR, 1, 1
PRINTVL ARR:1
ARRAYREMOVE ARR, 1
PRINTVL ARR:1
A = 1
B = 2
SWAP A, B
PRINTVL A
PRINTVL B
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 4 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if out[0].Text != "30" || out[1].Text != "0" || out[2].Text != "2" || out[3].Text != "1" {
		t.Fatalf("unexpected values: %+v", out)
	}
}

func TestCharacterAndUICommands(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
ADDCHARA 100
ADDCHARA 200
FINDCHARA 200
PRINTVL RESULT
SWAPCHARA 0, 1
GETCHARA 0
PRINTVL RESULT
ALIGNMENT "RIGHT"
CURRENTALIGN
PRINTVL RESULT
SETCOLOR "ABCDEF"
GETCOLOR
PRINTVL RESULT
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 4 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if out[0].Text != "1" || out[1].Text != "200" || out[2].Text != "RIGHT" || out[3].Text != "ABCDEF" {
		t.Fatalf("unexpected command outputs: %+v", out)
	}
}

func TestUtilityCommandFamilies(t *testing.T) {
	files := map[string]string{
		"MAIN.ERH": `
#DIMS PARTS, 8
`,
		"MAIN.ERB": `
@TITLE
BARL 3, 10, 10
SPLIT "a|b|c", "|", PARTS
PRINTVL RESULT
PRINTVL PARTS:1
ESCAPE "a+b"
PRINTVL RESULTS
ENCODETOUNI "AZ"
PRINTVL RESULT
PUTFORM X{1+1}
PRINTVL SAVEDATA_TEXT
A = 777
RESETGLOBAL
PRINTVL A
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 7 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if out[0].Text != "[***.......]" {
		t.Fatalf("unexpected BAR output: %q", out[0].Text)
	}
	if out[1].Text != "3" || out[2].Text != "b" {
		t.Fatalf("unexpected SPLIT outputs: %q %q", out[1].Text, out[2].Text)
	}
	if out[3].Text != "a\\+b" {
		t.Fatalf("unexpected ESCAPE output: %q", out[3].Text)
	}
	if out[4].Text != "2" || out[5].Text != "X2" {
		t.Fatalf("unexpected ENCODE/PUTFORM outputs: %q %q", out[4].Text, out[5].Text)
	}
	if out[6].Text != "0" {
		t.Fatalf("unexpected RESETGLOBAL effect: %q", out[6].Text)
	}
}

func TestDebugClear(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
PRINTL a
DEBUGCLEAR
PRINTL b
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 1 || out[0].Text != "b" {
		t.Fatalf("unexpected DEBUGCLEAR behavior: %+v", out)
	}
}
