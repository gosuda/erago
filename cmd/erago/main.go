package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func main() {
	base := flag.String("base", ".", "base path containing script files and save files")
	dir := flag.String("dir", "", "deprecated alias for -base")
	entry := flag.String("entry", "TITLE", "entry function")
	flag.Parse()

	resolvedBase := strings.TrimSpace(*base)
	if resolvedBase == "" {
		resolvedBase = "."
	}
	legacyDir := strings.TrimSpace(*dir)
	if legacyDir != "" && (strings.TrimSpace(*base) == "" || *base == ".") {
		resolvedBase = legacyDir
	}

	cfg := appConfig{
		base:  resolvedBase,
		entry: *entry,
	}

	p := tea.NewProgram(newModel(cfg))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}
