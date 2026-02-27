package main

import (
	"flag"
	"fmt"
	"os"

	eruntime "github.com/gosuda/erago/runtime"
)

func main() {
	kind := flag.String("kind", "var", "save kind: var|chara")
	in := flag.String("in", "", "input file path")
	out := flag.String("out", "", "output file path")
	to := flag.String("to", "json", "output format: json|binary")
	flag.Parse()

	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/savecodec -kind var|chara -in <input> -out <output> -to json|binary")
		os.Exit(2)
	}
	if err := eruntime.ConvertDatFile(*kind, *in, *out, *to); err != nil {
		fmt.Fprintf(os.Stderr, "convert failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("converted %s -> %s (%s)\n", *in, *out, *to)
}
