package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosuda/erago"
)

func main() {
	dir := flag.String("dir", ".", "script directory")
	entry := flag.String("entry", "TITLE", "entry function")
	flag.Parse()

	files, err := loadScripts(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load scripts: %v\n", err)
		os.Exit(1)
	}

	vm, err := erago.Compile(files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "compile: %v\n", err)
		os.Exit(1)
	}

	out, err := vm.Run(*entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runtime: %v\n", err)
		os.Exit(1)
	}

	for _, o := range out {
		if o.NewLine {
			fmt.Println(o.Text)
		} else {
			fmt.Print(o.Text)
		}
	}
}

func loadScripts(root string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToUpper(filepath.Ext(path))
		if ext != ".ERB" && ext != ".ERH" && ext != ".CSV" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = filepath.Base(path)
		}
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no script files found under %s", root)
	}
	return files, nil
}
