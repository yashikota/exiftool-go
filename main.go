package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yashikota/exiftool-go/pkg/exiftool"
)

func main() {
	stdout, stderr, exitCode, err := exiftool.RunCLIWithStdin(context.Background(), os.Args[1:], os.Stdin)
	if stdout != "" {
		fmt.Fprint(os.Stdout, stdout)
	}
	if stderr != "" {
		fmt.Fprint(os.Stderr, stderr)
		if stderr[len(stderr)-1] != '\n' {
			fmt.Fprintln(os.Stderr)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	os.Exit(exitCode)
}
