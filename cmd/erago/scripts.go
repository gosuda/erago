package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

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
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = filepath.Base(path)
		}
		rel = filepath.ToSlash(rel)
		if !isInGameScriptTree(rel) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = string(b)
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

func isInGameScriptTree(rel string) bool {
	parts := strings.SplitN(rel, "/", 2)
	top := strings.ToUpper(strings.TrimSpace(parts[0]))
	return top == "ERB" || top == "ERH" || top == "CSV"
}
