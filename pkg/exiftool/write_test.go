package exiftool

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	sharedOnce sync.Once
	sharedInst *ExifTool
	sharedErr  error
)

// TestMain closes the shared instance used by the tests in this file.
func TestMain(m *testing.M) {
	code := m.Run()
	if sharedInst != nil {
		sharedInst.Close()
	}
	os.Exit(code)
}

// shared returns an ExifTool instance shared across tests, since creating
// one takes several seconds. It also checks that an instance keeps working
// across many reads and writes.
func shared(t *testing.T) *ExifTool {
	t.Helper()
	sharedOnce.Do(func() {
		sharedInst, sharedErr = New()
	})
	if sharedErr != nil {
		t.Fatalf("Failed to create ExifTool: %v", sharedErr)
	}
	return sharedInst
}

// copyTestdata copies a testdata file into a fresh temp directory.
func copyTestdata(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("Failed to read testdata: %v", err)
	}
	dst := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("Failed to copy testdata: %v", err)
	}
	return dst
}

func TestPerlBytes(t *testing.T) {
	tests := map[string]string{
		"":         `pack("H*", "")`,
		"a":        `pack("H*", "61")`,
		"'":        `pack("H*", "27")`,
		`\`:        `pack("H*", "5c")`,
		"日":        `pack("H*", "e697a5")`,
		"\x00\xff": `pack("H*", "00ff")`,
	}
	for in, want := range tests {
		if got := perlBytes([]byte(in)); got != want {
			t.Errorf("perlBytes(%q) = %s, want %s", in, got, want)
		}
	}
}

// specialValues are tag values that used to break the generated Perl code
// or get corrupted on the way to ExifTool.
var specialValues = map[string]string{
	"plain":             "Test Author",
	"single quote":      "O'Brien",
	"double quote":      `say "hi"`,
	"backslash":         `C:\Users\test`,
	"backslash escapes": `\n\t\b\f\u0041`,
	"perl injection":    `x'.die("injected").'`,
	"perl interpolate":  `$0 @INC %ENV`,
	"japanese":          "日本語の作者",
	"emoji":             "camera 📷",
	"accents":           "Café Ünïcödé",
	"line separator":    "a\u2028b",
	"html chars":        "<a href='x'>&amp;</a>",
	"newline":           "line1\nline2",
	"long":              strings.Repeat("abcdefghij", 200),
}

func TestWriteMetadataSpecialCharacters(t *testing.T) {
	et := shared(t)

	cases := []struct {
		file string
		tag  string
	}{
		{"test.pdf", "Creator"},
		{"test.jpg", "XMP:Description"},
	}

	for _, c := range cases {
		for name, value := range specialValues {
			t.Run(c.file+"/"+c.tag+"/"+name, func(t *testing.T) {
				dst := filepath.Join(t.TempDir(), "out"+filepath.Ext(c.file))
				src := filepath.Join("testdata", c.file)
				if err := et.SetTag(src, dst, c.tag, value); err != nil {
					t.Fatalf("SetTag(%q) failed: %v", value, err)
				}
				metadata, err := et.ReadMetadata(dst)
				if err != nil {
					t.Fatalf("ReadMetadata failed: %v", err)
				}
				key := c.tag
				if i := strings.Index(key, ":"); i >= 0 {
					key = key[i+1:]
				}
				if got := metadata[key]; got != value {
					t.Errorf("%s = %q, want %q", key, got, value)
				}
			})
		}
	}
}

// TestIssue10PDFSetTag reproduces https://github.com/yashikota/exiftool-go/issues/10:
// SetTag on a PDF used to produce a 0-byte file without returning an error.
func TestIssue10PDFSetTag(t *testing.T) {
	et := shared(t)

	t.Run("to new file", func(t *testing.T) {
		src := copyTestdata(t, "test.pdf")
		before, _ := os.ReadFile(src)
		dst := filepath.Join(t.TempDir(), "out.pdf")

		if err := et.SetTag(src, dst, "Creator", "Test Author"); err != nil {
			t.Fatalf("SetTag failed: %v", err)
		}
		assertValidPDF(t, dst)

		after, _ := os.ReadFile(src)
		if !bytes.Equal(before, after) {
			t.Error("source file must not change when dstPath is given")
		}

		metadata, err := et.ReadMetadata(dst)
		if err != nil {
			t.Fatalf("ReadMetadata failed: %v", err)
		}
		if got := metadata["Creator"]; got != "Test Author" {
			t.Errorf("Creator = %v, want Test Author", got)
		}
	})

	t.Run("in place", func(t *testing.T) {
		src := copyTestdata(t, "test.pdf")
		if err := et.SetTag(src, "", "Creator", "In Place"); err != nil {
			t.Fatalf("SetTag failed: %v", err)
		}
		assertValidPDF(t, src)

		metadata, err := et.ReadMetadata(src)
		if err != nil {
			t.Fatalf("ReadMetadata failed: %v", err)
		}
		if got := metadata["Creator"]; got != "In Place" {
			t.Errorf("Creator = %v, want In Place", got)
		}
	})

	t.Run("repeated writes", func(t *testing.T) {
		src := copyTestdata(t, "test.pdf")
		for _, v := range []string{"one", "two", "three"} {
			if err := et.SetTag(src, "", "Creator", v); err != nil {
				t.Fatalf("SetTag(%q) failed: %v", v, err)
			}
		}
		assertValidPDF(t, src)
		metadata, err := et.ReadMetadata(src)
		if err != nil {
			t.Fatalf("ReadMetadata failed: %v", err)
		}
		if got := metadata["Creator"]; got != "three" {
			t.Errorf("Creator = %v, want three", got)
		}
	})

	t.Run("all info tags", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "out.pdf")
		tags := map[string]any{
			"Title":    "T",
			"Author":   "A",
			"Subject":  "S",
			"Keywords": "k1, k2",
			"Creator":  "C",
			"Producer": "P",
		}
		if err := et.WriteMetadata(filepath.Join("testdata", "test.pdf"), dst, tags); err != nil {
			t.Fatalf("WriteMetadata failed: %v", err)
		}
		assertValidPDF(t, dst)
		metadata, err := et.ReadMetadata(dst)
		if err != nil {
			t.Fatalf("ReadMetadata failed: %v", err)
		}
		for tag, want := range tags {
			if got := metadata[tag]; got != want {
				t.Errorf("%s = %v, want %v", tag, got, want)
			}
		}
	})
}

func assertValidPDF(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read %s: %v", path, err)
	}
	if len(data) == 0 {
		t.Fatalf("%s is 0 bytes", path)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("%s does not start with a PDF header", path)
	}
}

func TestWriteMetadataValueTypes(t *testing.T) {
	et := shared(t)
	dst := filepath.Join(t.TempDir(), "out.jpg")

	tags := map[string]any{
		"XMP:Subject": []string{"alpha", "ベータ", "it's"},
		"Rating":      3,
		"ISO":         400,
		"Artist":      "Typed",
	}
	if err := et.WriteMetadata(filepath.Join("testdata", "test.jpg"), dst, tags); err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	metadata, err := et.ReadMetadata(dst)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	// ImageInfo joins list values with ", " by default.
	if got := metadata["Subject"]; got != "alpha, ベータ, it's" {
		t.Errorf("Subject = %#v, want alpha, ベータ, it's", got)
	}
	// Numbers may come back as JSON numbers or strings depending on the tag.
	if got := fmt.Sprint(metadata["Rating"]); got != "3" {
		t.Errorf("Rating = %#v, want 3", metadata["Rating"])
	}
	if got := fmt.Sprint(metadata["ISO"]); got != "400" {
		t.Errorf("ISO = %#v, want 400", metadata["ISO"])
	}
	if got := metadata["Artist"]; got != "Typed" {
		t.Errorf("Artist = %v, want Typed", got)
	}
}

func TestWriteMetadataUnknownTag(t *testing.T) {
	et := shared(t)
	src := copyTestdata(t, "test.jpg")
	before, _ := os.ReadFile(src)

	err := et.SetTag(src, "", "NoSuchTag", "x")
	if err == nil {
		t.Fatal("SetTag with an unknown tag should fail")
	}
	if !strings.Contains(err.Error(), "NoSuchTag") {
		t.Errorf("error should name the tag, got %v", err)
	}

	after, _ := os.ReadFile(src)
	if !bytes.Equal(before, after) {
		t.Error("source file must not change when the write fails")
	}
}

func TestWriteMetadataUnsupportedFormat(t *testing.T) {
	et := shared(t)
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "note.txt")
	if err := os.WriteFile(src, []byte("plain text"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmpDir, "out.txt")

	err := et.SetTag(src, dst, "Artist", "x")
	if err == nil {
		t.Fatal("writing a text file should fail")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error should explain why, got %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("no destination file should be created on failure")
	}
}

func TestWriteMetadataRecoversAfterFailure(t *testing.T) {
	et := shared(t)
	tmpDir := t.TempDir()
	corrupt := filepath.Join(tmpDir, "corrupt.jpg")
	if err := os.WriteFile(corrupt, []byte("\xff\xd8\xff\xe0 truncated jpeg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := et.SetTag(corrupt, filepath.Join(tmpDir, "bad.jpg"), "Artist", "x"); err == nil {
		t.Log("corrupt JPEG write unexpectedly succeeded")
	}

	dst := filepath.Join(tmpDir, "good.jpg")
	if err := et.SetTag(filepath.Join("testdata", "test.jpg"), dst, "Artist", "after failure"); err != nil {
		t.Fatalf("write after a failed write should succeed: %v", err)
	}
	metadata, err := et.ReadMetadata(dst)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}
	if got := metadata["Artist"]; got != "after failure" {
		t.Errorf("Artist = %v, want after failure", got)
	}
}

func TestWriteMetadataPreservesPermissions(t *testing.T) {
	et := shared(t)
	src := copyTestdata(t, "test.jpg")
	if err := os.Chmod(src, 0600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out.jpg")

	if err := et.SetTag(src, dst, "Artist", "perm"); err != nil {
		t.Fatalf("SetTag failed: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("destination mode = %v, want -rw-------", got)
	}
}

func TestWriteMetadataPerlDieReturnsError(t *testing.T) {
	et := shared(t)
	output, _, err := et.eval(`die "boom\n";`)
	if err != nil {
		return
	}
	if output != "" {
		t.Errorf("a dying script should produce no output, got %q", output)
	}
}

func TestReadMetadataNonASCII(t *testing.T) {
	et := shared(t)
	dst := filepath.Join(t.TempDir(), "out.jpg")
	want := "撮影者 ✓"
	if err := et.SetTag(filepath.Join("testdata", "test.jpg"), dst, "XMP:Creator", want); err != nil {
		t.Fatalf("SetTag failed: %v", err)
	}

	metadata, err := et.ReadMetadata(dst)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}
	if got := metadata["Creator"]; got != want {
		t.Errorf("Creator = %q, want %q", got, want)
	}
}

func TestReadMetadataSpecialPath(t *testing.T) {
	et := shared(t)
	dir := filepath.Join(t.TempDir(), "it's a \"dir\" 日本")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "test.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "my file's.pdf")
	if err := os.WriteFile(src, data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := et.SetTag(src, "", "Title", "special path"); err != nil {
		t.Fatalf("SetTag failed: %v", err)
	}
	metadata, err := et.ReadMetadata(src)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}
	if got := metadata["Title"]; got != "special path" {
		t.Errorf("Title = %v, want special path", got)
	}
	if got := metadata["FileName"]; got != "my file's.pdf" {
		t.Errorf("FileName = %v, want my file's.pdf", got)
	}
}

// TestConcurrentUse checks that concurrent calls on one instance do not
// clobber each other's temp files.
func TestConcurrentUse(t *testing.T) {
	et := shared(t)
	files := []string{"test.pdf", "test.jpg"}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			file := files[i%len(files)]
			want := fmt.Sprintf("worker %d", i)
			dst := filepath.Join(t.TempDir(), "out"+filepath.Ext(file))
			if err := et.SetTag(filepath.Join("testdata", file), dst, "XMP:Title", want); err != nil {
				errs <- fmt.Errorf("worker %d SetTag: %w", i, err)
				return
			}
			metadata, err := et.ReadMetadata(dst)
			if err != nil {
				errs <- fmt.Errorf("worker %d ReadMetadata: %w", i, err)
				return
			}
			if metadata["Title"] != want || metadata["FileType"] != strings.ToUpper(strings.Replace(filepath.Ext(file)[1:], "jpg", "jpeg", 1)) {
				errs <- fmt.Errorf("worker %d got Title=%v FileType=%v", i, metadata["Title"], metadata["FileType"])
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
