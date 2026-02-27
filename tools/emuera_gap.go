package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

func main() {
	emueraCodePath := "/tmp/Emuera/Emuera/GameProc/Function/BuiltInFunctionCode.cs"
	knownPath := "parser/known_commands_gen.go"
	if len(os.Args) > 1 {
		emueraCodePath = os.Args[1]
	}

	enumSet, err := extractFunctionCodes(emueraCodePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read emuera function codes: %v\n", err)
		os.Exit(1)
	}
	knownSet, err := extractKnownCommands(knownPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read known commands: %v\n", err)
		os.Exit(1)
	}

	missing := diff(enumSet, knownSet)
	extra := diff(knownSet, enumSet)

	fmt.Printf("emuera enum count: %d\n", len(enumSet))
	fmt.Printf("erago known command count: %d\n", len(knownSet))
	fmt.Printf("missing in erago: %d\n", len(missing))
	for _, n := range missing {
		fmt.Println("  - " + n)
	}
	fmt.Printf("extra in erago: %d\n", len(extra))
	for _, n := range extra {
		fmt.Println("  + " + n)
	}
}

func extractFunctionCodes(path string) (map[string]struct{}, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src := string(b)
	start := strings.Index(src, "enum FunctionCode")
	if start < 0 {
		return nil, fmt.Errorf("enum FunctionCode not found")
	}
	open := strings.Index(src[start:], "{")
	if open < 0 {
		return nil, fmt.Errorf("enum opening brace not found")
	}
	open += start
	close := strings.LastIndex(src, "}")
	if close < 0 || close <= open {
		return nil, fmt.Errorf("enum closing brace not found")
	}
	body := src[open+1 : close]
	reName := regexp.MustCompile(`^[A-Z0-9_]+$`)
	set := map[string]struct{}{}
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		line = strings.TrimSuffix(line, ",")
		if i := strings.Index(line, "="); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" || line == "__NULL__" {
			continue
		}
		if reName.MatchString(line) {
			set[line] = struct{}{}
		}
	}
	return set, nil
}

func extractKnownCommands(path string) (map[string]struct{}, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`"([A-Z0-9_]+)"\s*:\s*\{\}`)
	set := map[string]struct{}{}
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		set[m[1]] = struct{}{}
	}
	return set, nil
}

func diff(base, comp map[string]struct{}) []string {
	out := make([]string, 0)
	for n := range base {
		if _, ok := comp[n]; !ok {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
