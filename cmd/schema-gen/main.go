package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/internal/config"
)

func main() {
	output := flag.String("output", "schema/depengine.schema.json", "output path")
	flag.Parse()
	data, err := config.GenerateJSONSchema()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", *output, err)
		os.Exit(1)
	}
}
