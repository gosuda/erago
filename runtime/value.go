package eruntime

import "strconv"

type ValueKind int

const (
	IntKind ValueKind = iota
	StringKind
)

type Value struct {
	kind ValueKind
	i    int64
	s    string
}

func Int(v int64) Value {
	return Value{kind: IntKind, i: v}
}

func Str(v string) Value {
	return Value{kind: StringKind, s: v}
}

func (v Value) Kind() ValueKind {
	return v.kind
}

func (v Value) Int64() int64 {
	if v.kind == IntKind {
		return v.i
	}
	i, err := strconv.ParseInt(v.s, 10, 64)
	if err != nil {
		return 0
	}
	return i
}

func (v Value) String() string {
	if v.kind == StringKind {
		return v.s
	}
	return strconv.FormatInt(v.i, 10)
}

func (v Value) Truthy() bool {
	if v.kind == StringKind {
		return v.s != ""
	}
	return v.i != 0
}
