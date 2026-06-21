package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
	if !strings.Contains(groupOut, "[File]") || !strings.Contains(groupOut, "File Type") {
		t.Fatalf("-G output = %s", groupOut)
	}
}

func TestCLIExifToolCompatibleWriteOptions(t *testing.T) {
	bin := buildCLI(t)
	src := filepath.Join("pkg", "exiftool", "testdata", "test.pdf")
	tmpDir := t.TempDir()
	pdf := filepath.Join(tmpDir, "test.pdf")
	copyFile(t, src, pdf)

	cmd := exec.Command(bin, "-Author=test", pdf)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(pdf + "_original"); err != nil {
		t.Fatalf("backup was not created: %v", err)
	}

	out, err := exec.Command(bin, "-s", "-Author", pdf).CombinedOutput()
	if err != nil {
		t.Fatalf("read back failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Author") || !strings.Contains(string(out), "test") {
		t.Fatalf("author was not written: %s", out)
	}
}

func TestCLIOverwriteOriginalSkipsBackup(t *testing.T) {
	bin := buildCLI(t)
	src := filepath.Join("pkg", "exiftool", "testdata", "test.pdf")
	tmpDir := t.TempDir()
	pdf := filepath.Join(tmpDir, "test.pdf")
	copyFile(t, src, pdf)

	cmd := exec.Command(bin, "-overwrite_original", "-Author=test", pdf)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write failed: %v\n%s", err, out)
	}

	if _, err := os.Stat(pdf + "_original"); !os.IsNotExist(err) {
		t.Fatalf("backup should not exist, stat err = %v", err)
	}
}

func TestCLITagsFromFileOption(t *testing.T) {
	bin := buildCLI(t)
	src := filepath.Join("pkg", "exiftool", "testdata", "test.pdf")
	tmpDir := t.TempDir()
	from := filepath.Join(tmpDir, "from.pdf")
	to := filepath.Join(tmpDir, "to.pdf")
	copyFile(t, src, from)
	copyFile(t, src, to)

	if out, err := exec.Command(bin, "-overwrite_original", "-Author=source author", from).CombinedOutput(); err != nil {
		t.Fatalf("source write failed: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin, "-overwrite_original", "-tagsFromFile", from, "-Author", to).CombinedOutput(); err != nil {
		t.Fatalf("tagsFromFile failed: %v\n%s", err, out)
	}

	out, err := exec.Command(bin, "-s", "-Author", to).CombinedOutput()
	if err != nil {
		t.Fatalf("read back failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "source author") {
		t.Fatalf("Author was not copied: %s", out)
	}
}

func TestCLIArgfileFromStdin(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "-@", "-")
	cmd.Stdin = bytes.NewBufferString("-ver\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("-@ - failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Fatalf("-@ - returned empty output")
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

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
