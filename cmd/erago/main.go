package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	base := flag.String("base", ".", "base path containing script files and save files")
	dir := flag.String("dir", "", "deprecated alias for -base")
	entry := flag.String("entry", "TITLE", "entry function")
	savef := flag.String("savefmt", "json", "SAVEVAR/SAVECHARA format: json|binary|both")
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
		savef: *savef,
	}

	p := tea.NewProgram(newModel(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}
