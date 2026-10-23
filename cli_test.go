package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestMain removes the shared CLI binary after the run.
func TestMain(m *testing.M) {
	code := m.Run()
	if builtBin != "" {
		os.RemoveAll(filepath.Dir(builtBin))
	}
	os.Exit(code)
}

type cliResult struct {
	stdout, stderr string
	exitCode       int
}

// runCLI runs the binary in dir (the repo root if empty) and returns its output.
func runCLI(t *testing.T, dir string, stdin io.Reader, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(buildCLI(t), args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := cliResult{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		res.exitCode = exitErr.ExitCode()
	case err != nil:
		t.Fatalf("failed to run %v: %v", args, err)
	}
	return res
}

// mustRunCLI runs the binary and fails the test on a non-zero exit status.
func mustRunCLI(t *testing.T, dir string, args ...string) string {
	t.Helper()
	res := runCLI(t, dir, nil, args...)
	if res.exitCode != 0 {
		t.Fatalf("%v exited %d\nstdout: %s\nstderr: %s", args, res.exitCode, res.stdout, res.stderr)
	}
	return res.stdout
}

// testdataCopy copies a testdata file into dir and returns the new path.
func testdataCopy(t *testing.T, dir, name, as string) string {
	t.Helper()
	dst := filepath.Join(dir, as)
	copyFile(t, filepath.Join("pkg", "exiftool", "testdata", name), dst)
	return dst
}

// readTag returns the value of one tag as printed by -s3.
func readTag(t *testing.T, dir, tag, file string) string {
	t.Helper()
	return strings.TrimRight(mustRunCLI(t, dir, "-s3", "-"+tag, file), "\n")
}

func TestCLIExitCodes(t *testing.T) {
	jpg := filepath.Join("pkg", "exiftool", "testdata", "test.jpg")

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"version", []string{"-ver"}, 0},
		{"read", []string{"-FileType", jpg}, 0},
		{"missing file", []string{"no-such-file.jpg"}, 1},
		{"write unsupported file", []string{"-Artist=x", "main.go"}, 1},
		{"if condition fails", []string{"-if", "$FileType eq 'PNG'", jpg}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runCLI(t, "", nil, tt.args...)
			if res.exitCode != tt.want {
				t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s", res.exitCode, tt.want, res.stdout, res.stderr)
			}
		})
	}
}

// TestCLIArgumentRoundTrip checks that arguments reach ExifTool unchanged.
// Quotes and backslashes used to break the generated Perl code, and
// non-ASCII text was double-encoded.
func TestCLIArgumentRoundTrip(t *testing.T) {
	values := map[string]string{
		"single quote":   "O'Brien",
		"double quote":   `say "hi"`,
		"windows path":   `C:\Users\test\photo.jpg`,
		"json escapes":   `\n\t\u0041`,
		"perl injection": `x'.die("injected").'`,
		"perl sigils":    `$0 @ARGV %ENV`,
		"japanese":       "日本語の作者",
		"emoji":          "camera 📷",
		"accents":        "Café Ünïcödé",
	}

	tmpDir := t.TempDir()
	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			pdf := testdataCopy(t, tmpDir, "test.pdf", strings.ReplaceAll(name, " ", "_")+".pdf")
			mustRunCLI(t, "", "-overwrite_original", "-Author="+value, pdf)
			if got := readTag(t, "", "Author", pdf); got != value {
				t.Errorf("Author = %q, want %q", got, value)
			}
		})
	}
}

func TestCLIJSONOutputUnicode(t *testing.T) {
	tmpDir := t.TempDir()
	pdf := testdataCopy(t, tmpDir, "test.pdf", "test.pdf")
	mustRunCLI(t, "", "-overwrite_original", "-Title=タイトル", "-Author=O'Brien", pdf)

	var got []map[string]any
	if err := json.Unmarshal([]byte(mustRunCLI(t, "", "-j", "-Title", "-Author", pdf)), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 || got[0]["Title"] != "タイトル" || got[0]["Author"] != "O'Brien" {
		t.Errorf("JSON = %v", got)
	}
}

// TestCLIIssue10PDFWrite covers https://github.com/yashikota/exiftool-go/issues/10
// through the CLI: PDF writes must produce a valid, non-empty file.
func TestCLIIssue10PDFWrite(t *testing.T) {
	tmpDir := t.TempDir()
	src := testdataCopy(t, tmpDir, "test.pdf", "src.pdf")
	out := filepath.Join(tmpDir, "out.pdf")

	mustRunCLI(t, "", "-o", out, "-Creator=Test Author", src)
	assertPDF(t, out)
	if got := readTag(t, "", "Creator", out); got != "Test Author" {
		t.Errorf("Creator = %q, want Test Author", got)
	}

	mustRunCLI(t, "", "-overwrite_original", "-Creator=In Place", src)
	assertPDF(t, src)
	if got := readTag(t, "", "Creator", src); got != "In Place" {
		t.Errorf("Creator = %q, want In Place", got)
	}
}

func assertPDF(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("%s is not a PDF (%d bytes)", path, len(data))
	}
}

func TestCLIPaths(t *testing.T) {
	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, "it's a \"dir\" 日本")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	jpg := testdataCopy(t, dir, "test.jpg", "my photo's.jpg")

	t.Run("absolute path outside working directory", func(t *testing.T) {
		otherDir := t.TempDir()
		if got := readTag(t, otherDir, "FileName", jpg); got != "my photo's.jpg" {
			t.Errorf("FileName = %q", got)
		}
	})

	t.Run("relative path from working directory", func(t *testing.T) {
		if got := readTag(t, dir, "FileType", "my photo's.jpg"); got != "JPEG" {
			t.Errorf("FileType = %q", got)
		}
	})

	t.Run("write to absolute output in a new directory", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "new", "sub", "out.jpg")
		mustRunCLI(t, t.TempDir(), "-o", out, "-Artist=abs", jpg)
		if got := readTag(t, "", "Artist", out); got != "abs" {
			t.Errorf("Artist = %q", got)
		}
	})

	t.Run("recursive with extension filter", func(t *testing.T) {
		root := t.TempDir()
		sub := filepath.Join(root, "a", "b")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatal(err)
		}
		testdataCopy(t, root, "test.jpg", "one.jpg")
		testdataCopy(t, sub, "test.jpg", "two.jpg")
		testdataCopy(t, sub, "test.pdf", "three.pdf")

		out := mustRunCLI(t, "", "-q", "-r", "-ext", "jpg", "-p", "$FileName", root)
		names := strings.Fields(out)
		sort.Strings(names)
		if len(names) != 2 || names[0] != "one.jpg" || names[1] != "two.jpg" {
			t.Errorf("recursive output = %q", out)
		}
	})
}

func TestCLIStdinFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("pkg", "exiftool", "testdata", "test.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, "", bytes.NewReader(data), "-s3", "-FileType", "-")
	if res.exitCode != 0 || strings.TrimSpace(res.stdout) != "JPEG" {
		t.Errorf("stdin read: exit=%d stdout=%q stderr=%q", res.exitCode, res.stdout, res.stderr)
	}
}

func TestCLIExecuteMultipleCommands(t *testing.T) {
	jpg := filepath.Join("pkg", "exiftool", "testdata", "test.jpg")
	pdf := filepath.Join("pkg", "exiftool", "testdata", "test.pdf")
	out := mustRunCLI(t, "", "-s3", "-FileType", jpg, "-execute", "-s3", "-FileType", pdf)
	if got := strings.Fields(out); len(got) != 2 || got[0] != "JPEG" || got[1] != "PDF" {
		t.Errorf("output = %q, want JPEG then PDF", out)
	}
}

// TestCLIStayOpenStreams checks that -stay_open answers each command as it
// arrives instead of buffering all output until exit.
func TestCLIStayOpenStreams(t *testing.T) {
	cmd := exec.Command(buildCLI(t), "-stay_open", "True", "-@", "-")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	lines := make(chan string)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	readUntilReady := func() []string {
		var got []string
		timeout := time.After(2 * time.Minute)
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("stdout closed early, got %q", got)
				}
				if line == "{ready}" {
					return got
				}
				got = append(got, line)
			case <-timeout:
				t.Fatalf("no {ready} while the process is still open, got %q", got)
			}
		}
	}

	jpg := filepath.Join("pkg", "exiftool", "testdata", "test.jpg")
	io.WriteString(stdin, "-s3\n-FileType\n"+jpg+"\n-execute\n")
	if got := readUntilReady(); len(got) != 1 || got[0] != "JPEG" {
		t.Errorf("first command output = %q", got)
	}
	io.WriteString(stdin, "-ver\n-execute\n")
	if got := readUntilReady(); len(got) != 1 || got[0] == "" {
		t.Errorf("second command output = %q", got)
	}

	io.WriteString(stdin, "-stay_open\nFalse\n")
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Errorf("process exited with %v", err)
	}
}

func TestCLIConditionAndFormatting(t *testing.T) {
	jpg := filepath.Join("pkg", "exiftool", "testdata", "test.jpg")

	out := mustRunCLI(t, "", "-if", "$ImageWidth > 50", "-p", "$FileName is ${ImageWidth}x$ImageHeight", jpg)
	if strings.TrimSpace(out) != "test.jpg is 100x68" {
		t.Errorf("-p output = %q", out)
	}

	out = mustRunCLI(t, "", "-csv", "-FileType", "-ImageWidth", jpg)
	if !strings.Contains(out, "SourceFile,FileType,ImageWidth") || !strings.Contains(out, "JPEG,100") {
		t.Errorf("-csv output = %q", out)
	}
}
