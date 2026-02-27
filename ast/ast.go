package ast

type Program struct {
	Defines    map[string]Expr
	Functions  map[string]*Function
	Order      []string
	CSVFiles   map[string]string
	StringVars map[string]struct{}
	VarDecls   []VarDecl
}

type VarDecl struct {
	Name      string
	IsString  bool
	Dims      []int
	Scope     string // global|local|dynamic
	IsRef     bool
	IsDynamic bool
}

type Function struct {
	Name     string
	Args     []Arg
	Body     *Thunk
	VarDecls []VarDecl
}

type Arg struct {
	Name    string
	Target  VarRef
	Default Expr
}

type Thunk struct {
	Statements []Statement
	LabelMap   map[string]int
}

type Statement interface {
	isStatement()
}

type PrintStmt struct {
	Expr    Expr
	NewLine bool
}

func (PrintStmt) isStatement() {}

type AssignStmt struct {
	Target VarRef
	Op     string
	Expr   Expr
}

func (AssignStmt) isStatement() {}

type IncDecStmt struct {
	Target VarRef
	Op     string
	Pre    bool
}

func (IncDecStmt) isStatement() {}

type IfStmt struct {
	Branches []IfBranch
	Else     *Thunk
}

func (IfStmt) isStatement() {}

type IfBranch struct {
	Cond Expr
	Body *Thunk
}

type GotoStmt struct {
	Label string
}

func (GotoStmt) isStatement() {}

type WhileStmt struct {
	Cond Expr
	Body *Thunk
}

func (WhileStmt) isStatement() {}

type DoWhileStmt struct {
	Body *Thunk
	Cond Expr
}

func (DoWhileStmt) isStatement() {}

type RepeatStmt struct {
	Count Expr
	Body  *Thunk
}

func (RepeatStmt) isStatement() {}

type ForStmt struct {
	Var    string
	Target VarRef
	Init   Expr
	Limit  Expr
	Step   Expr
	Body   *Thunk
}

func (ForStmt) isStatement() {}

type BreakStmt struct{}

func (BreakStmt) isStatement() {}

type ContinueStmt struct{}

func (ContinueStmt) isStatement() {}

type CommandStmt struct {
	Name string
	Arg  string
}

func (CommandStmt) isStatement() {}

type SelectCaseStmt struct {
	Target   Expr
	Branches []SelectCaseBranch
	Else     *Thunk
}

func (SelectCaseStmt) isStatement() {}

type SelectCaseBranch struct {
	Conditions []CaseCondition
	Body       *Thunk
}

type CaseCondition struct {
	Kind string // equal|range|compare
	Expr Expr
	From Expr
	To   Expr
	Op   string
}

type PrintDataStmt struct {
	Command string
	Items   []DataItem
}

func (PrintDataStmt) isStatement() {}

type StrDataStmt struct {
	Target VarRef
	Items  []DataItem
}

func (StrDataStmt) isStatement() {}

type DataItem struct {
	Kind string // data|dataform
	Raw  string
}

type CallStmt struct {
	Name string
	Args []Expr
}

func (CallStmt) isStatement() {}

type ReturnStmt struct {
	Values []Expr
}

func (ReturnStmt) isStatement() {}

type BeginStmt struct {
	Keyword string
}

func (BeginStmt) isStatement() {}

type QuitStmt struct{}

func (QuitStmt) isStatement() {}

type Expr interface {
	isExpr()
}

type IntLit struct {
	Value int64
}

func (IntLit) isExpr() {}

type StringLit struct {
	Value string
}

func (StringLit) isExpr() {}

type VarRef struct {
	Name  string
	Index []Expr
}

func (VarRef) isExpr() {}

type UnaryExpr struct {
	Op   string
	Expr Expr
}

func (UnaryExpr) isExpr() {}

type BinaryExpr struct {
	Op    string
	Left  Expr
	Right Expr
}

func (BinaryExpr) isExpr() {}

type TernaryExpr struct {
	Cond  Expr
	True  Expr
	False Expr
}

func (TernaryExpr) isExpr() {}

type CallExpr struct {
	Name string
	Args []Expr
}

func (CallExpr) isExpr() {}

type EmptyLit struct{}

func (EmptyLit) isExpr() {}

type IncDecExpr struct {
	Target VarRef
	Op     string
	Post   bool
}

func (IncDecExpr) isExpr() {}
