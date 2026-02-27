package main

import (
  "fmt"
  "io/fs"
  "os"
  "path/filepath"
  "strings"

  "github.com/gosuda/erago/ast"
  "github.com/gosuda/erago/parser"
)

func load(root string) (map[string]string, error) {
  files := map[string]string{}
  err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
    if err != nil { return err }
    if d.IsDir() { return nil }
    ext := strings.ToUpper(filepath.Ext(path))
    if ext != ".ERB" && ext != ".ERH" && ext != ".CSV" { return nil }
    rel, err := filepath.Rel(root, path)
    if err != nil { rel = filepath.Base(path) }
    rel = filepath.ToSlash(rel)
    top := strings.ToUpper(strings.SplitN(rel, "/", 2)[0])
    if top != "ERB" && top != "ERH" && top != "CSV" { return nil }
    b, err := os.ReadFile(path)
    if err != nil { return err }
    files[rel] = string(b)
    return nil
  })
  return files, err
}

func main() {
  files, err := load(`era_files/3_eratoho_reverse`)
  if err != nil { panic(err) }
  prog, err := parser.ParseProgram(files)
  if err != nil { panic(err) }
  fn := prog.Functions["COM_VITALITY"]
  if fn == nil { panic("missing COM_VITALITY") }
  fmt.Printf("fn=%s stmts=%d\n", fn.Name, len(fn.Body.Statements))
  for i, st := range fn.Body.Statements {
    switch s := st.(type) {
    case ast.IfStmt:
      fmt.Printf("pc %d If branches=%d elseNil=%v\n", i, len(s.Branches), s.Else == nil)
    case ast.AssignStmt:
      fmt.Printf("pc %d Assign target=%s idx=%d op=%s\n", i, s.Target.Name, len(s.Target.Index), s.Op)
    case ast.ReturnStmt:
      fmt.Printf("pc %d Return vals=%d\n", i, len(s.Values))
    case ast.CallStmt:
      fmt.Printf("pc %d Call %s args=%d\n", i, s.Name, len(s.Args))
    default:
      fmt.Printf("pc %d %T\n", i, st)
    }
    if i >= 20 { break }
  }
}
