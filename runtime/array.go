package eruntime

import (
	"fmt"
	"strconv"
	"strings"
)

type ArrayVar struct {
	IsString  bool
	IsDynamic bool
	Dims      []int
	Data      map[string]Value
}

func newArrayVar(isString bool, isDynamic bool, dims []int) *ArrayVar {
	cp := make([]int, len(dims))
	copy(cp, dims)
	if len(cp) == 0 {
		cp = []int{1}
	}
	return &ArrayVar{
		IsString:  isString,
		IsDynamic: isDynamic,
		Dims:      cp,
		Data:      map[string]Value{},
	}
}

func (a *ArrayVar) defaultValue() Value {
	if a.IsString {
		return Str("")
	}
	return Int(0)
}

func (a *ArrayVar) key(index []int64) (string, error) {
	if len(index) == 0 {
		return "0", nil
	}
	if !a.IsDynamic && len(index) > len(a.Dims) {
		return "", fmt.Errorf("too many indices: got %d, max %d", len(index), len(a.Dims))
	}
	if a.IsDynamic && len(index) > len(a.Dims) {
		for i := len(a.Dims); i < len(index); i++ {
			a.Dims = append(a.Dims, int(index[i])+1)
		}
	}
	parts := make([]string, len(index))
	for i, v := range index {
		if v < 0 {
			return "", fmt.Errorf("index %d out of range: %d", i, v)
		}
		if a.IsDynamic {
			if i >= len(a.Dims) {
				a.Dims = append(a.Dims, int(v)+1)
			}
			if int(v) >= a.Dims[i] {
				a.Dims[i] = int(v) + 1
			}
		} else if i < len(a.Dims) && int(v) >= a.Dims[i] {
			return "", fmt.Errorf("index %d out of range: %d >= %d", i, v, a.Dims[i])
		}
		parts[i] = strconv.FormatInt(v, 10)
	}
	return strings.Join(parts, ":"), nil
}

func (a *ArrayVar) Get(index []int64) (Value, error) {
	k, err := a.key(index)
	if err != nil {
		return Value{}, err
	}
	if v, ok := a.Data[k]; ok {
		return v, nil
	}
	return a.defaultValue(), nil
}

func (a *ArrayVar) Set(index []int64, v Value) error {
	k, err := a.key(index)
	if err != nil {
		return err
	}
	if a.IsString {
		a.Data[k] = Str(v.String())
	} else {
		a.Data[k] = Int(v.Int64())
	}
	return nil
}
