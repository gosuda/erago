package erago_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosuda/erago"
	"github.com/gosuda/erago/parser"
	eruntime "github.com/gosuda/erago/runtime"
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
	vm.SetSaveDir(t.TempDir())
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

func TestCSVNamedIndexInArrayAccess(t *testing.T) {
	files := map[string]string{
		"MAIN.ERH": `
#DIM FLAG, 32
`,
		"MAIN.ERB": `
@TITLE
FLAG:MODE = 7
FLAG:CHARA_TOTAL = 11
PRINTVL FLAG:5
PRINTVL FLAG:8
PRINTVL FLAG:MODE
PRINTVL FLAG:CHARA_TOTAL
QUIT
`,
		"FLAG.CSV": `
5,MODE
8,CHARA_TOTAL
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
	if out[0].Text != "7" || out[1].Text != "11" || out[2].Text != "7" || out[3].Text != "11" {
		t.Fatalf("unexpected CSV named index behavior: %+v", out)
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

func TestEmueraCompatCommandsBaseline(t *testing.T) {
	files := map[string]string{
		"MAIN.ERH": `
#DIM ARR, 4
#DIM REF R
`,
		"MAIN.ERB": `
@TITLE
ARR:0 = 3
ARR:1 = 1
ARR:2 = 2
ARRAYSORT ARR
PRINTVL ARR:0
ARRAYCOPY BARR, ARR
VARSIZE BARR, 0
PRINTVL RESULT
REF R, ARR:1
PRINTVL R
CALL F
PRINTVL RESULT
ASSERT 1
TRYCALLLIST MISSING_A, MISSING_B
TWAIT
AWAIT
QUIT

@F
RETURNFORM hello {1+1}
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
		t.Fatalf("unexpected output count: %d (%+v)", len(out), out)
	}
	if out[0].Text != "0" {
		t.Fatalf("unexpected ARRAYSORT result: %q", out[0].Text)
	}
	if out[1].Text != "4" {
		t.Fatalf("unexpected VARSIZE result: %q", out[1].Text)
	}
	if out[2].Text != "1" {
		t.Fatalf("unexpected REF result: %q", out[2].Text)
	}
	if out[3].Text != "hello 2" {
		t.Fatalf("unexpected RETURNFORM result: %q", out[3].Text)
	}
}

func TestEmueraMethodArrayAndStringHelpers(t *testing.T) {
	files := map[string]string{
		"MAIN.ERH": `
#DIM ARR, 5
#DIMS SARR, 4
`,
		"MAIN.ERB": `
@TITLE
ARR:0 = 1
ARR:1 = 2
ARR:2 = 2
ARR:3 = 5
SARR:0 = "aa"
SARR:1 = "bb"
SARR:2 = "ab"

SUMARRAY ARR
PRINTVL RESULT
MATCH ARR, 2
PRINTVL RESULT
MAXARRAY ARR
PRINTVL RESULT
MINARRAY ARR
PRINTVL RESULT
FINDELEMENT ARR, 2
PRINTVL RESULT
FINDLASTELEMENT ARR, 2
PRINTVL RESULT
INRANGEARRAY ARR, 2, 4
PRINTVL RESULT
GROUPMATCH 3, 3, 2, 3
PRINTVL RESULT
NOSAMES "a", "b", "a"
PRINTVL RESULT
ALLSAMES 7, 7, 7
PRINTVL RESULT
REPLACE "a-b-c", "-", "+"
PRINTVL RESULTS
STRCOUNT "ababa", "ba"
PRINTVL RESULT
STRJOIN "|", "x", "y", "z"
PRINTVL RESULTS
STRFORM "A={1+2}"
PRINTVL RESULTS
CHARATU "ABC", 1
PRINTVL RESULTS
CONVERT 255, 16
PRINTVL RESULTS
ISNUMERIC "12.5"
PRINTVL RESULT
ISNUMERIC "a12"
PRINTVL RESULT
TOFULL "A 1!"
PRINTVL RESULTS
TOHALF "Ａ　１！"
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
	if len(out) != 20 {
		t.Fatalf("unexpected output count: %d (%+v)", len(out), out)
	}
	expect := []string{
		"10", "2", "5", "0", "1", "2", "2", "2", "0", "1",
		"a+b+c", "2", "x|y|z", "A=3", "B", "ff", "1", "0", "Ａ　１！", "A 1!",
	}
	for i, exp := range expect {
		if out[i].Text != exp {
			t.Fatalf("unexpected output at %d: got=%q want=%q", i, out[i].Text, exp)
		}
	}
}

func TestEmueraMethodGetNum(t *testing.T) {
	files := map[string]string{
		"ABL.CSV": "0,힘\n1,민첩\n",
		"MAIN.ERB": `
@TITLE
GETNUM ABL, "민첩"
PRINTVL RESULT
GETNUMB "ABL", "힘"
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
	if len(out) != 2 {
		t.Fatalf("unexpected output count: %d (%+v)", len(out), out)
	}
	if out[0].Text != "1" || out[1].Text != "0" {
		t.Fatalf("unexpected GETNUM outputs: %+v", out)
	}
}

func TestTryCatchAndFuncEndFuncFlow(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
A = 0
TRYCGOTO MISSING_LABEL
A = 1
CATCH
A = 2
ENDCATCH
PRINTVL A

TRYCCALL UNKNOWN_FUNC
A = 10
CATCH
A = 11
ENDCATCH
PRINTVL A

TRYCCALL SET_A2
A = 100
CATCH
A = 101
ENDCATCH
PRINTVL A

TRYCALLLIST
FUNC UNKNOWN1()
FUNC SET_A()
ENDFUNC
PRINTVL A

TRYGOTOLIST
FUNC NOPE
FUNC OK
ENDFUNC
A = 999
$OK
PRINTVL A
QUIT

@SET_A
A = 7
RETURN

@SET_A2
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
	if len(out) < 5 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	got := []string{
		out[len(out)-5].Text,
		out[len(out)-4].Text,
		out[len(out)-3].Text,
		out[len(out)-2].Text,
		out[len(out)-1].Text,
	}
	want := []string{"2", "11", "100", "7", "7"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected output at %d: got=%q want=%q all=%+v", i, got[i], want[i], out)
		}
	}
}

func TestInputStateMachineAndDefaults(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
INPUT 5
A = RESULT
INPUT
B = RESULT
INPUTS "abc"
S = RESULTS
ONEINPUT 98
C = RESULT
TINPUT 10, 42, 1, "timeout"
D = RESULT
TINPUTS 10, "xyz", 1, "oops"
T = RESULTS
WAIT
PRINTVL A
PRINTVL B
PRINTVL S
PRINTVL C
PRINTVL D
PRINTVL T
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	vm.EnqueueInput("12", "", "", "789")
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) < 6 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	got := []string{
		out[len(out)-6].Text,
		out[len(out)-5].Text,
		out[len(out)-4].Text,
		out[len(out)-3].Text,
		out[len(out)-2].Text,
		out[len(out)-1].Text,
	}
	want := []string{"12", "0", "abc", "7", "42", "xyz"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected input flow at %d: got=%q want=%q all=%+v", i, got[i], want[i], out)
		}
	}
}

func TestPrintWConsumesQueuedInputBeforeInput(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
PRINTW hello
INPUT
PRINTVL RESULT
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	vm.EnqueueInput("7")
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("unexpected empty output")
	}
	last := out[len(out)-1].Text
	if last != "0" {
		t.Fatalf("PRINTW should consume queued input before INPUT: got=%q out=%+v", last, out)
	}
}

func TestPrintDataWConsumesQueuedInputBeforeInput(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
PRINTDATAW
DATA apple
ENDDATA
INPUT
PRINTVL RESULT
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	vm.EnqueueInput("5")
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("unexpected empty output")
	}
	last := out[len(out)-1].Text
	if last != "0" {
		t.Fatalf("PRINTDATAW should consume queued input before INPUT: got=%q out=%+v", last, out)
	}
}

func TestSaveVarLoadVarCompat(t *testing.T) {
	files := map[string]string{
		"MAIN.ERH": `
#DIM ARR, 4
#DIMS SS, 4
`,
		"MAIN.ERB": `
@TITLE
A = 7
ARR:1 = 11
SS:2 = "hi"
SAVEVAR "case1", "mes", A, ARR, SS
A = 0
ARR:1 = 0
SS:2 = ""
LOADVAR "case1"
PRINTVL A
PRINTVL ARR:1
PRINTVL SS:2
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	tmp := t.TempDir()
	vm.SetSaveDir(tmp)
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 3 || out[0].Text != "7" || out[1].Text != "11" || out[2].Text != "hi" {
		t.Fatalf("unexpected SAVEVAR/LOADVAR outputs: %+v", out)
	}
	if _, err := os.Stat(filepath.Join(tmp, "var_case1.dat")); err != nil {
		t.Fatalf("expected var save file: %v", err)
	}
}

func TestSaveCharaLoadCharaCompat(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
ADDCHARA 101
ADDCHARA 202
SAVECHARA "party", "memo", 0
DELALLCHARA
LOADCHARA "party"
PRINTVL CHARANUM
GETCHARA 0
PRINTVL RESULT
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	tmp := t.TempDir()
	vm.SetSaveDir(tmp)
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 2 || out[0].Text != "1" || out[1].Text != "101" {
		t.Fatalf("unexpected SAVECHARA/LOADCHARA outputs: %+v", out)
	}
	if _, err := os.Stat(filepath.Join(tmp, "chara_party.dat")); err != nil {
		t.Fatalf("expected chara save file: %v", err)
	}
}

func TestSaveVarBinaryMode(t *testing.T) {
	files := map[string]string{
		"MAIN.ERH": "#DIM ARR, 3\n",
		"MAIN.ERB": `
@TITLE
A = 9
ARR:1 = 22
SAVEVAR "bin1", "m"
A = 0
ARR:1 = 0
LOADVAR "bin1"
PRINTVL A
PRINTVL ARR:1
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := vm.SetDatSaveFormat("binary"); err != nil {
		t.Fatalf("set format failed: %v", err)
	}
	tmp := t.TempDir()
	vm.SetSaveDir(tmp)
	out, err := vm.Run("TITLE")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out) != 2 || out[0].Text != "9" || out[1].Text != "22" {
		t.Fatalf("unexpected binary save/load outputs: %+v", out)
	}
	b, err := os.ReadFile(filepath.Join(tmp, "var_bin1.dat"))
	if err != nil {
		t.Fatalf("read dat failed: %v", err)
	}
	if !eruntime.IsEraBinaryData(b) {
		t.Fatalf("expected binary dat format")
	}
}

func TestSaveVarBothModeWritesJsonCompanion(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
A = 3
SAVEVAR "both1", "m"
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := vm.SetDatSaveFormat("both"); err != nil {
		t.Fatalf("set format failed: %v", err)
	}
	tmp := t.TempDir()
	vm.SetSaveDir(tmp)
	if _, err := vm.Run("TITLE"); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "var_both1.dat")); err != nil {
		t.Fatalf("expected binary dat: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "var_both1.json")); err != nil {
		t.Fatalf("expected json companion: %v", err)
	}
}

func TestDatConverterVarJsonBinaryRoundtrip(t *testing.T) {
	files := map[string]string{
		"MAIN.ERH": "#DIM ARR, 4\n",
		"MAIN.ERB": `
@TITLE
A = 15
ARR:2 = 88
SAVEVAR "conv1", "m"
QUIT
`,
	}
	vm, err := erago.Compile(files)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	tmp := t.TempDir()
	vm.SetSaveDir(tmp)
	if _, err := vm.Run("TITLE"); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	jsonDat := filepath.Join(tmp, "var_conv1.dat")
	binDat := filepath.Join(tmp, "var_conv1_bin.dat")
	jsonOut := filepath.Join(tmp, "var_conv1_out.json")
	if err := eruntime.ConvertDatFile("var", jsonDat, binDat, "binary"); err != nil {
		t.Fatalf("json->binary convert failed: %v", err)
	}
	b, err := os.ReadFile(binDat)
	if err != nil {
		t.Fatalf("read converted binary failed: %v", err)
	}
	if !eruntime.IsEraBinaryData(b) {
		t.Fatalf("expected converted binary dat")
	}
	if err := eruntime.ConvertDatFile("var", binDat, jsonOut, "json"); err != nil {
		t.Fatalf("binary->json convert failed: %v", err)
	}
	if _, err := os.Stat(jsonOut); err != nil {
		t.Fatalf("expected converted json file: %v", err)
	}
}

func TestScopedArgReferenceByFunctionSubID(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
TALENT:1:150 = 1
CALL ERASE_MEMORY(1)
QUIT

@ERASE_MEMORY(ARG)
CALL SELF_KOJO_K11
RETURN 0

@SELF_KOJO_K11
IF TALENT:(ARG@ERASE_MEMORY):150
    PRINTL ok
ELSE
    PRINTL ng
ENDIF
RETURN 0
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
	if out[0].Text != "ok" {
		t.Fatalf("unexpected output: %+v", out[0])
	}
}

func TestHTMLStringFunctions(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
LOCALS = <font color='red'>Hello</font> World
A = HTML_STRINGLEN(LOCALS)
PRINTVL A
LOCALS:1 = HTML_SUBSTRING(LOCALS, 0, 5)
PRINTFORML %LOCALS:1%
LOCALS:2 = HTML_SUBSTRING(LOCALS, 5, 10)
PRINTFORML %LOCALS:2%
B = HTML_STRINGLINES("<p>Line1</p><p>Line2</p>")
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
	if len(out) < 4 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if out[0].Text != "11" {
		t.Fatalf("HTML_STRINGLEN expected 11, got %q", out[0].Text)
	}
	if out[3].Text != "1" {
		t.Fatalf("HTML_STRINGLINES expected 1, got %q", out[3].Text)
	}
}

func TestDynamicVariableFunctions(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
A = 123
LOCALS = "A"
B = GETVAR(LOCALS)
PRINTVL B
C = EXISTVAR("A")
PRINTVL C
D = EXISTVAR("NONEXISTENT")
PRINTVL D
E = ISDEFINED("TITLE")
PRINTVL E
F = ISDEFINED("NONEXISTENT_FUNC")
PRINTVL F
G = EXISTFUNCTION("TITLE")
PRINTVL G
H = "A"
SETVAR H, 456
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
	expected := []string{"123", "1", "0", "1", "0", "1", "456"}
	for i, exp := range expected {
		if i >= len(out) {
			t.Fatalf("missing output at index %d", i)
		}
		if out[i].Text != exp {
			t.Fatalf("output[%d] expected %q, got %q", i, exp, out[i].Text)
		}
	}
}

func TestRegexpMatch(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
LOCALS = "Hello123World"
LOCALS:1 = "Hello"
A = REGEXPMATCH(LOCALS, LOCALS:1)
PRINTFORML A=%A%
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
	if len(out) < 1 {
		t.Fatalf("missing output")
	}
	t.Logf("output: %q", out[0].Text)
	if out[0].Text != "A=1" {
		t.Fatalf("expected A=1, got %q", out[0].Text)
	}
}

func TestEnumFunctions(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
LOCALS = ENUMFUNCBEGINSWITH("TI")
PRINTFORML %LOCALS%
LOCALS:1 = ENUMVARBEGINSWITH("RES")
PRINTFORML %LOCALS:1%
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
	if len(out) < 2 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if !containsString(out[0].Text, "TITLE") {
		t.Fatalf("ENUMFUNCBEGINSWITH should contain TITLE, got %q", out[0].Text)
	}
	if !containsString(out[1].Text, "RESULT") {
		t.Fatalf("ENUMVARBEGINSWITH should contain RESULT, got %q", out[1].Text)
	}
}

func containsString(s, substr string) bool {
	return len(s) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestMathFunctions(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
A = CBRT(27)
B = LOG(100)
C = LOG10(1000)
D = EXPONENT(3)
E = EXPONENT(2, 10)
PRINTFORML CBRT(27)=%A%
PRINTFORML LOG(100)=%B%
PRINTFORML LOG10(1000)=%C%
PRINTFORML EXPONENT(3)=%D%
PRINTFORML EXPONENT(2,10)=%E%
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
	expected := []string{"CBRT(27)=3", "LOG10(1000)=3", "EXPONENT(3)=3", "EXPONENT(2,10)=1024"}
	for _, exp := range expected {
		found := false
		for _, o := range out {
			if strings.Contains(o.Text, exp) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected output containing %q, got %v", exp, out)
		}
	}
}

func TestColorFunctions(t *testing.T) {
	files := map[string]string{
		"MAIN.ERB": `
@TITLE
A = COLOR_FROMNAME("red")
B = COLOR_FROMNAME("blue")
C = COLOR_FROMRGB(255, 128, 0)
PRINTFORML COLOR_FROMNAME("red")=%A%
PRINTFORML COLOR_FROMNAME("blue")=%B%
PRINTFORML COLOR_FROMRGB(255,128,0)=%C%
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
	expected := []struct {
		key   string
		value string
	}{
		{"COLOR_FROMNAME(\"red\")", "16711680"},
		{"COLOR_FROMNAME(\"blue\")", "255"},
		{"COLOR_FROMRGB(255,128,0)", "16744448"},
	}
	for _, exp := range expected {
		found := false
		for _, o := range out {
			if strings.Contains(o.Text, exp.key+"="+exp.value) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected output containing %s=%s, got %v", exp.key, exp.value, out)
		}
	}
}
