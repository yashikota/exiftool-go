package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/yashikota/exiftool-go/pkg/exiftool"
)

var (
	Version string
)

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		usage()
		os.Exit(1)
	}

	if cfg.showHelp {
		usage()
		return
	}

	if cfg.showExifToolVersion {
		et, err := exiftool.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		etVer, _ := et.Version()
		et.Close()
		fmt.Println(etVer)
		return
	}

	if cfg.showVersion {
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

	if len(cfg.files) < 1 {
		usage()
		os.Exit(1)
	}

	// Create ExifTool instance
	et, err := exiftool.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing ExifTool: %v\n", err)
		os.Exit(1)
	}
	defer et.Close()

	if len(cfg.tags) > 0 {
		writeMetadata(et, cfg)
		return
	}

	readMetadata(et, cfg)
}

type cliConfig struct {
	jsonOutput          bool
	shortOutput         bool
	groupNames          bool
	duplicates          bool
	quiet               bool
	preserveTime        bool
	overwriteOriginal   bool
	showHelp            bool
	showVersion         bool
	showExifToolVersion bool
	outputPath          string
	tags                map[string]any
	readTags            []string
	excludeTags         []string
	files               []string
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [options] <image_file> [image_file...]\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "A pure Go ExifTool\n\n")
	fmt.Fprintf(os.Stderr, "Options:\n")
	fmt.Fprintf(os.Stderr, "  -json\n        Output as JSON\n")
	fmt.Fprintf(os.Stderr, "  -j\n        Same as -json\n")
	fmt.Fprintf(os.Stderr, "  -version\n        Show version\n")
	fmt.Fprintf(os.Stderr, "  -ver\n        Show ExifTool version\n")
	fmt.Fprintf(os.Stderr, "  -s\n        Short output format with tag names\n")
	fmt.Fprintf(os.Stderr, "  -G\n        Print group name for each tag\n")
	fmt.Fprintf(os.Stderr, "  -a\n        Allow duplicate tags to be extracted\n")
	fmt.Fprintf(os.Stderr, "  -q\n        Quiet output\n")
	fmt.Fprintf(os.Stderr, "  -P\n        Preserve file modification time when writing\n")
	fmt.Fprintf(os.Stderr, "  -h, -help\n        Show help\n")
	fmt.Fprintf(os.Stderr, "  -o <file>\n        Write metadata to a new file (single input file only)\n")
	fmt.Fprintf(os.Stderr, "  -TAG\n        Extract specified tag\n")
	fmt.Fprintf(os.Stderr, "  --TAG\n        Exclude specified tag\n")
	fmt.Fprintf(os.Stderr, "  -TAG=value\n        Write metadata tag value\n")
	fmt.Fprintf(os.Stderr, "  -overwrite_original\n        Overwrite original without creating a _original backup\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  %s photo.jpg\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -json photo.jpg\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s photo1.jpg photo2.jpg\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -Author=\"Test Author\" -Title=\"Test Title\" document.pdf\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -o output.pdf -Author=\"Test Author\" document.pdf\n", os.Args[0])
}

func parseArgs(args []string) (cliConfig, error) {
	cfg := cliConfig{
		tags: make(map[string]any),
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-json", "-j":
			cfg.jsonOutput = true
		case "-version":
			cfg.showVersion = true
		case "-ver":
			cfg.showExifToolVersion = true
		case "-s":
			cfg.shortOutput = true
		case "-G":
			cfg.groupNames = true
		case "-a":
			cfg.duplicates = true
		case "-q":
			cfg.quiet = true
		case "-P":
			cfg.preserveTime = true
		case "-h", "-help", "--help":
			cfg.showHelp = true
		case "-overwrite_original", "-overwrite_original_in_place":
			cfg.overwriteOriginal = true
		case "-o":
			i++
			if i >= len(args) {
				return cfg, fmt.Errorf("-o requires an output file")
			}
			cfg.outputPath = args[i]
		default:
			if strings.HasPrefix(arg, "-o=") {
				cfg.outputPath = strings.TrimPrefix(arg, "-o=")
				if cfg.outputPath == "" {
					return cfg, fmt.Errorf("-o requires an output file")
				}
				continue
			}

			if strings.HasPrefix(arg, "--") {
				tag := strings.TrimPrefix(arg, "--")
				if tag == "" {
					return cfg, fmt.Errorf("unknown option %s", arg)
				}
				cfg.excludeTags = append(cfg.excludeTags, tag)
				continue
			}

			if strings.HasPrefix(arg, "-") {
				tag, value, ok := strings.Cut(strings.TrimPrefix(arg, "-"), "=")
				if tag == "" {
					return cfg, fmt.Errorf("unknown option %s", arg)
				}
				if ok {
					cfg.tags[tag] = value
				} else {
					cfg.readTags = append(cfg.readTags, tag)
				}
				continue
			}

			cfg.files = append(cfg.files, arg)
		}
	}

	if cfg.outputPath != "" && len(cfg.files) > 1 {
		return cfg, fmt.Errorf("-o can only be used with one input file")
	}

	if cfg.outputPath != "" && len(cfg.tags) == 0 {
		return cfg, fmt.Errorf("-o requires at least one -TAG=value option")
	}

	return cfg, nil
}

func readMetadata(et *exiftool.ExifTool, cfg cliConfig) {
	// Store results for multiple files
	var allResults []map[string]any

	for _, filePath := range cfg.files {
		tags, err := et.ReadMetadataTags(filePath, exiftool.ReadOptions{
			Tags:       cfg.readTags,
			Exclude:    cfg.excludeTags,
			Duplicates: cfg.duplicates,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", filePath, err)
			continue
		}

		if cfg.jsonOutput {
			metadata := tagsToMap(tags)
			metadata["SourceFile"] = filePath
			allResults = append(allResults, metadata)
		} else if !cfg.quiet {
			printMetadata(filePath, tags, cfg)
		}
	}

	if cfg.jsonOutput && len(allResults) > 0 {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		encoder.Encode(allResults)
	}
}

func writeMetadata(et *exiftool.ExifTool, cfg cliConfig) {
	for _, filePath := range cfg.files {
		dstPath := ""
		if cfg.outputPath != "" {
			dstPath = cfg.outputPath
		}

		var modTime time.Time
		if cfg.preserveTime {
			if info, err := os.Stat(filePath); err == nil {
				modTime = info.ModTime()
			}
		}

		if dstPath == "" && !cfg.overwriteOriginal {
			if err := backupOriginal(filePath); err != nil {
				fmt.Fprintf(os.Stderr, "Error backing up %s: %v\n", filePath, err)
				continue
			}
		}

		if err := et.WriteMetadata(filePath, dstPath, cfg.tags); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", filePath, err)
			continue
		}

		output := filePath
		if dstPath != "" {
			output = dstPath
		}
		if !modTime.IsZero() {
			_ = os.Chtimes(output, modTime, modTime)
		}

		if !cfg.quiet {
			fmt.Printf("Wrote metadata to %s\n", output)
		}
	}
}

func backupOriginal(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	backupPath := filePath + "_original"
	if _, err := os.Stat(backupPath); err == nil {
		return fmt.Errorf("%s already exists", filepath.Base(backupPath))
	} else if !os.IsNotExist(err) {
		return err
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	return os.WriteFile(backupPath, data, info.Mode().Perm())
}

func tagsToMap(tags []exiftool.TagInfo) map[string]any {
	metadata := make(map[string]any, len(tags))
	for _, tag := range tags {
		metadata[tag.Name] = tag.Value
	}
	return metadata
}

func printMetadata(filePath string, tags []exiftool.TagInfo, cfg cliConfig) {
	if len(cfg.files) > 1 {
		fmt.Printf("======== %s\n", filePath)
	}

	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Name < tags[j].Name
	})

	for _, tag := range tags {
		if str, ok := tag.Value.(string); ok && str == "[binary data]" {
			continue // Skip binary data
		}

		name := tag.Name
		if cfg.groupNames && tag.Group != "" {
			name = "[" + tag.Group + "] " + name
		}
		if cfg.shortOutput {
			fmt.Printf("%-32s : %v\n", name, tag.Value)
		} else {
			fmt.Printf("%-32s : %v\n", name, tag.Value)
		}
	}

	if len(cfg.files) > 1 {
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
