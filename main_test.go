package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgsWriteTag(t *testing.T) {
	cfg, err := parseArgs([]string{"-Author=test", "-Title=Test PDF", "document.pdf"})
	if err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	if len(cfg.files) != 1 || cfg.files[0] != "document.pdf" {
		t.Fatalf("files = %#v, want document.pdf", cfg.files)
	}

	if cfg.tags["Author"] != "test" {
		t.Fatalf("Author = %#v, want test", cfg.tags["Author"])
	}

	if cfg.tags["Title"] != "Test PDF" {
		t.Fatalf("Title = %#v, want Test PDF", cfg.tags["Title"])
	}
}

func TestParseArgsReadOptions(t *testing.T) {
	cfg, err := parseArgs([]string{"-j", "-s", "-G", "-a", "--FileSize", "-Author", "document.pdf"})
	if err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	if !cfg.jsonOutput || !cfg.shortOutput || !cfg.groupNames || !cfg.duplicates {
		t.Fatalf("read flags were not parsed: %#v", cfg)
	}

	if len(cfg.readTags) != 1 || cfg.readTags[0] != "Author" {
		t.Fatalf("readTags = %#v, want Author", cfg.readTags)
	}

	if len(cfg.excludeTags) != 1 || cfg.excludeTags[0] != "FileSize" {
		t.Fatalf("excludeTags = %#v, want FileSize", cfg.excludeTags)
	}
}

func TestParseArgsOutputFile(t *testing.T) {
	cfg, err := parseArgs([]string{"-o", "output.pdf", "-Author=test", "input.pdf"})
	if err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	if cfg.outputPath != "output.pdf" {
		t.Fatalf("outputPath = %q, want output.pdf", cfg.outputPath)
	}
}

func TestParseArgsOutputRequiresSingleInput(t *testing.T) {
	_, err := parseArgs([]string{"-o", "output.pdf", "-Author=test", "one.pdf", "two.pdf"})
	if err == nil {
		t.Fatal("parseArgs should fail when -o is used with multiple inputs")
	}
}

func TestCLIExifToolCompatibleReadOptions(t *testing.T) {
	bin := buildCLI(t)
	pdf := filepath.Join("pkg", "exiftool", "testdata", "test.pdf")

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	if got := strings.TrimSpace(run("-ver")); got == "" || strings.Contains(got, "exiftool-go") {
		t.Fatalf("-ver output = %q, want raw ExifTool version", got)
	}

	jsonOut := run("-j", pdf)
	if !strings.Contains(jsonOut, `"SourceFile"`) {
		t.Fatalf("-j output did not contain SourceFile: %s", jsonOut)
	}

	shortOut := run("-s", "-FileType", pdf)
	if !strings.Contains(shortOut, "FileType") || strings.Contains(shortOut, "File Type") {
		t.Fatalf("-s output = %s", shortOut)
	}

	groupOut := run("-G", "-FileType", pdf)
	if !strings.Contains(groupOut, "[File] FileType") {
		t.Fatalf("-G output = %s", groupOut)
	}
}

func TestCLIWriteCreatesOriginalBackupByDefault(t *testing.T) {
	bin := buildCLI(t)
	src := filepath.Join("pkg", "exiftool", "testdata", "test.pdf")
	tmpDir := t.TempDir()
	pdf := filepath.Join(tmpDir, "test.pdf")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if err := os.WriteFile(pdf, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cmd := exec.Command(bin, "-Author=test", pdf)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write failed: %v\n%s", err, out)
	}

	if _, err := os.Stat(pdf + "_original"); err != nil {
		t.Fatalf("backup was not created: %v", err)
	}
}

func TestCLIOverwriteOriginalSkipsBackup(t *testing.T) {
	bin := buildCLI(t)
	src := filepath.Join("pkg", "exiftool", "testdata", "test.pdf")
	tmpDir := t.TempDir()
	pdf := filepath.Join(tmpDir, "test.pdf")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if err := os.WriteFile(pdf, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cmd := exec.Command(bin, "-overwrite_original", "-Author=test", pdf)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write failed: %v\n%s", err, out)
	}

	if _, err := os.Stat(pdf + "_original"); !os.IsNotExist(err) {
		t.Fatalf("backup should not exist, stat err = %v", err)
	}
}

func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "exiftool-go")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}
