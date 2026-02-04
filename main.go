package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"sort"

	"github.com/yashikota/exiftool-go/pkg/exiftool"
)

var (
	Version    string
	jsonOutput = flag.Bool("json", false, "Output as JSON")
	showVer    = flag.Bool("version", false, "Show version")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <image_file> [image_file...]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "A pure Go ExifTool\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s photo.jpg\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -json photo.jpg\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s photo1.jpg photo2.jpg\n", os.Args[0])
	}
	flag.Parse()

	if *showVer {
		et, err := exiftool.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		etVer, _ := et.Version()
		et.Close()
		fmt.Printf("exiftool-go version %s (ExifTool %s)\n", getVersion(), etVer)
		return
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	// Create ExifTool instance
	et, err := exiftool.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing ExifTool: %v\n", err)
		os.Exit(1)
	}
	defer et.Close()

	// Store results for multiple files
	var allResults []map[string]any

	for _, filePath := range flag.Args() {
		metadata, err := et.ReadMetadata(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", filePath, err)
			continue
		}

		// Add source file path
		metadata["SourceFile"] = filePath

		if *jsonOutput {
			allResults = append(allResults, metadata)
		} else {
			printMetadata(filePath, metadata)
		}
	}

	if *jsonOutput && len(allResults) > 0 {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		encoder.Encode(allResults)
	}
}

func printMetadata(filePath string, metadata map[string]any) {
	if len(flag.Args()) > 1 {
		fmt.Printf("======== %s\n", filePath)
	}

	// Sort keys
	keys := make([]string, 0, len(metadata))
	for k := range metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := metadata[key]
		if str, ok := value.(string); ok && str == "[binary data]" {
			continue // Skip binary data
		}
		fmt.Printf("%-32s : %v\n", key, value)
	}

	if len(flag.Args()) > 1 {
		fmt.Println()
	}
}

func getVersion() string {
	Version := ""
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "(devel)" {
			return info.Main.Version
		}
		if v, ok := getVCSBuildVersion(info); ok {
			return v
		}
	}
	return "(unset)"
}

func getVCSBuildVersion(info *debug.BuildInfo) (string, bool) {
	var (
		revision string
		dirty    string
	)
	for _, v := range info.Settings {
		switch v.Key {
		case "vcs.revision":
			revision = v.Value
		case "vcs.modified":
			dirty = " (dirty)"
		}
	}
	if revision == "" {
		return "", false
	}
	return revision + dirty, true
}
