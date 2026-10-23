package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yashikota/exiftool-go/pkg/exiftool"
)

func main() {
	exitCode, err := exiftool.RunCLIWithIO(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	os.Exit(exitCode)
}
