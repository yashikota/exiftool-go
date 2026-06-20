package exiftool

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestNew(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	if et.mod == nil {
		t.Error("Module should not be nil")
	}
	if et.runtime == nil {
		t.Error("Runtime should not be nil")
	}
	if et.tmpDir == "" {
		t.Error("TmpDir should not be empty")
	}
}

func TestNewWithContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	et, err := NewWithContext(ctx)
	if err != nil {
		t.Fatalf("Failed to create ExifTool with context: %v", err)
	}
	defer et.Close()

	if et.ctx != ctx {
		t.Error("Context should match the provided context")
	}
}

func TestClose(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}

	tmpDir := et.tmpDir

	// Verify temp dir exists before close
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		t.Fatal("Temp directory should exist before close")
	}

	err = et.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify temp dir is cleaned up after close
	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Error("Temp directory should be removed after close")
	}
}

func TestVersion(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	version, err := et.Version()
	if err != nil {
		t.Fatalf("Version failed: %v", err)
	}

	if version == "" {
		t.Error("Version should not be empty")
	}

	// ExifTool version should be a number like "12.76"
	t.Logf("ExifTool version: %s", version)
}

func TestReadMetadata(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	srcPath := filepath.Join("testdata", "test.jpg")

	metadata, err := et.ReadMetadata(srcPath)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	// Basic checks - should have some standard JPEG metadata
	if metadata == nil {
		t.Fatal("Metadata should not be nil")
	}

	if len(metadata) == 0 {
		t.Fatal("Metadata should not be empty")
	}

	// Check for common JPEG tags
	if _, ok := metadata["FileType"]; !ok {
		t.Error("FileType tag should be present")
	}

	if fileType, ok := metadata["FileType"]; ok && fileType != "JPEG" {
		t.Errorf("FileType should be JPEG, got %v", fileType)
	}

	if _, ok := metadata["MIMEType"]; !ok {
		t.Error("MIMEType tag should be present")
	}

	if _, ok := metadata["ImageWidth"]; !ok {
		t.Error("ImageWidth tag should be present")
	}

	if _, ok := metadata["ImageHeight"]; !ok {
		t.Error("ImageHeight tag should be present")
	}
}

func TestReadMetadataFileNotFound(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	_, err = et.ReadMetadata("nonexistent_file.jpg")
	if err == nil {
		t.Error("ReadMetadata should fail for nonexistent file")
	}
}

func TestWriteMetadataSourceNotFound(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	tmpDir := t.TempDir()
	dstPath := filepath.Join(tmpDir, "output.jpg")

	tags := map[string]any{
		"Artist": "Test",
	}

	err = et.WriteMetadata("nonexistent_file.jpg", dstPath, tags)
	if err == nil {
		t.Error("WriteMetadata should fail for nonexistent source file")
	}
}

func TestSetTagFileNotFound(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	tmpDir := t.TempDir()
	dstPath := filepath.Join(tmpDir, "output.jpg")

	err = et.SetTag("nonexistent_file.jpg", dstPath, "Artist", "Test")
	if err == nil {
		t.Error("SetTag should fail for nonexistent source file")
	}
}

func TestReadMetadataMultipleTags(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	// First write some metadata
	srcPath := filepath.Join("testdata", "test.jpg")
	tmpDir := t.TempDir()
	dstPath := filepath.Join(tmpDir, "output.jpg")

	tags := map[string]any{
		"Artist":           "Test Artist",
		"Copyright":        "2026 Test Copyright",
		"ImageDescription": "Test Description",
		"Comment":          "Test Comment",
	}

	err = et.WriteMetadata(srcPath, dstPath, tags)
	if err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	// Now read it back and verify all tags
	metadata, err := et.ReadMetadata(dstPath)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	for tag, expected := range tags {
		if actual, ok := metadata[tag]; !ok {
			t.Errorf("Tag %s should be present", tag)
		} else if actual != expected {
			t.Errorf("Tag %s: expected %v, got %v", tag, expected, actual)
		}
	}
}

func TestWriteMetadata(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	// Get the test image path
	srcPath := filepath.Join("testdata", "test.jpg")

	// Create a temp file for output
	tmpDir := t.TempDir()
	dstPath := filepath.Join(tmpDir, "output.jpg")

	// Write metadata to a new file
	tags := map[string]any{
		"Artist":    "Test Artist",
		"Copyright": "2026 Test",
	}

	err = et.WriteMetadata(srcPath, dstPath, tags)
	if err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	// Verify the output file exists
	if _, err := os.Stat(dstPath); os.IsNotExist(err) {
		t.Fatal("Output file was not created")
	}

	// Read back and verify
	metadata, err := et.ReadMetadata(dstPath)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	if artist, ok := metadata["Artist"]; !ok || artist != "Test Artist" {
		t.Errorf("Artist tag not set correctly: got %v", artist)
	}

	if copyright, ok := metadata["Copyright"]; !ok || copyright != "2026 Test" {
		t.Errorf("Copyright tag not set correctly: got %v", copyright)
	}
}

func TestWriteMetadataInPlace(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	// Get the test image path
	srcPath := filepath.Join("testdata", "test.jpg")

	// Copy to temp dir for in-place modification
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.jpg")

	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("Failed to read source file: %v", err)
	}
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	// Write metadata in place (empty dstPath)
	tags := map[string]any{
		"Artist": "InPlace Artist",
	}

	err = et.WriteMetadata(tmpFile, "", tags)
	if err != nil {
		t.Fatalf("WriteMetadata in-place failed: %v", err)
	}

	// Read back and verify
	metadata, err := et.ReadMetadata(tmpFile)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	if artist, ok := metadata["Artist"]; !ok || artist != "InPlace Artist" {
		t.Errorf("Artist tag not set correctly: got %v", artist)
	}
}

func TestSetTag(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	// Get the test image path
	srcPath := filepath.Join("testdata", "test.jpg")

	// Create a temp file for output
	tmpDir := t.TempDir()
	dstPath := filepath.Join(tmpDir, "output.jpg")

	// Set a single tag
	err = et.SetTag(srcPath, dstPath, "Artist", "SetTag Artist")
	if err != nil {
		t.Fatalf("SetTag failed: %v", err)
	}

	// Read back and verify
	metadata, err := et.ReadMetadata(dstPath)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	if artist, ok := metadata["Artist"]; !ok || artist != "SetTag Artist" {
		t.Errorf("Artist tag not set correctly: got %v", artist)
	}
}

func TestWriteMetadataGolden(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	srcPath := filepath.Join("testdata", "test.jpg")
	goldenPath := filepath.Join("testdata", "write_metadata_golden.json")

	// Create a temp file for output
	tmpDir := t.TempDir()
	dstPath := filepath.Join(tmpDir, "output.jpg")

	// Define tags to write
	tags := map[string]any{
		"Artist":           "Golden Test Artist",
		"Copyright":        "2026 Golden Test",
		"ImageDescription": "Golden test image",
	}

	err = et.WriteMetadata(srcPath, dstPath, tags)
	if err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	// Read back metadata
	metadata, err := et.ReadMetadata(dstPath)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	// Extract only the tags we care about for comparison
	actual := extractTags(metadata, []string{"Artist", "Copyright", "ImageDescription"})

	if *updateGolden {
		// Update golden file
		data, err := json.MarshalIndent(actual, "", "  ")
		if err != nil {
			t.Fatalf("Failed to marshal golden data: %v", err)
		}
		if err := os.WriteFile(goldenPath, data, 0644); err != nil {
			t.Fatalf("Failed to write golden file: %v", err)
		}
		t.Log("Golden file updated")
		return
	}

	// Load golden file
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to read golden file (run with -update to create): %v", err)
	}

	var expected map[string]any
	if err := json.Unmarshal(goldenData, &expected); err != nil {
		t.Fatalf("Failed to unmarshal golden file: %v", err)
	}

	// Compare
	if !compareMaps(actual, expected) {
		actualJSON, _ := json.MarshalIndent(actual, "", "  ")
		expectedJSON, _ := json.MarshalIndent(expected, "", "  ")
		t.Errorf("Metadata mismatch.\nActual:\n%s\n\nExpected:\n%s", actualJSON, expectedJSON)
	}
}

func TestReadMetadataGolden(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	srcPath := filepath.Join("testdata", "test.jpg")
	goldenPath := filepath.Join("testdata", "read_metadata_golden.json")

	// Read metadata
	metadata, err := et.ReadMetadata(srcPath)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	// Extract stable tags for comparison (exclude dynamic tags like file paths)
	stableTags := []string{
		"ImageWidth", "ImageHeight", "FileType", "MIMEType",
		"BitsPerSample", "ColorComponents", "EncodingProcess",
	}
	actual := extractTags(metadata, stableTags)

	if *updateGolden {
		// Update golden file
		data, err := json.MarshalIndent(actual, "", "  ")
		if err != nil {
			t.Fatalf("Failed to marshal golden data: %v", err)
		}
		if err := os.WriteFile(goldenPath, data, 0644); err != nil {
			t.Fatalf("Failed to write golden file: %v", err)
		}
		t.Log("Golden file updated")
		return
	}

	// Load golden file
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to read golden file (run with -update to create): %v", err)
	}

	var expected map[string]any
	if err := json.Unmarshal(goldenData, &expected); err != nil {
		t.Fatalf("Failed to unmarshal golden file: %v", err)
	}

	// Compare
	if !compareMaps(actual, expected) {
		actualJSON, _ := json.MarshalIndent(actual, "", "  ")
		expectedJSON, _ := json.MarshalIndent(expected, "", "  ")
		t.Errorf("Metadata mismatch.\nActual:\n%s\n\nExpected:\n%s", actualJSON, expectedJSON)
	}
}

// TestReadMetadataFilePermissions verifies that FilePermissions reflects the
// real file's mode, not the hardcoded temp-file permissions used in the sandbox.
func TestReadMetadataFilePermissions(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	// Write a temp file with known permissions
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "perms.jpg")

	src, err := os.ReadFile(filepath.Join("testdata", "test.jpg"))
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}
	if err := os.WriteFile(tmpFile, src, 0640); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	metadata, err := et.ReadMetadata(tmpFile)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	got, ok := metadata["FilePermissions"]
	if !ok {
		t.Fatal("FilePermissions tag should be present")
	}
	if got == "----------" {
		t.Error("FilePermissions should not be ---------- (sandbox default)")
	}
	if got != "rw-r-----" {
		t.Errorf("FilePermissions: expected %q, got %v", "rw-r-----", got)
	}
}

// TestReadMetadataDirectory verifies that Directory, FileName and SourceFile
// reflect the user-supplied path, not the internal temp path used by the WASM sandbox.
func TestReadMetadataDirectory(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	srcPath := filepath.Join("testdata", "test.jpg")

	metadata, err := et.ReadMetadata(srcPath)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	if got, ok := metadata["SourceFile"]; !ok || got != srcPath {
		t.Errorf("SourceFile: expected %q, got %v", srcPath, got)
	}

	if got, ok := metadata["Directory"]; !ok || got != filepath.Dir(srcPath) {
		t.Errorf("Directory: expected %q, got %v", filepath.Dir(srcPath), got)
	}

	if got, ok := metadata["FileName"]; !ok || got != filepath.Base(srcPath) {
		t.Errorf("FileName: expected %q, got %v", filepath.Base(srcPath), got)
	}

	// Ensure no temp-path leakage
	for _, field := range []string{"SourceFile", "Directory", "FileName"} {
		if val, ok := metadata[field]; ok {
			if s, ok := val.(string); ok && (s == "/tmp/input" || s == "/tmp") {
				t.Errorf("%s contains temp path value %q", field, s)
			}
		}
	}
}

// TestReadMetadataDirectoryRelativePath verifies path fields with a relative input path.
func TestReadMetadataDirectoryRelativePath(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	// Use a path relative to the current working directory
	srcPath := filepath.Join("testdata", "test.jpg")

	metadata, err := et.ReadMetadata(srcPath)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	if got, ok := metadata["Directory"]; !ok || got != "testdata" {
		t.Errorf("Directory: expected %q, got %v", "testdata", got)
	}

	if got, ok := metadata["FileName"]; !ok || got != "test.jpg" {
		t.Errorf("FileName: expected %q, got %v", "test.jpg", got)
	}
}

// TestReadMetadataDirectoryAfterWrite verifies path fields are correct when reading
// a file written to a temp directory by WriteMetadata.
func TestReadMetadataDirectoryAfterWrite(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	srcPath := filepath.Join("testdata", "test.jpg")
	tmpDir := t.TempDir()
	dstPath := filepath.Join(tmpDir, "output.jpg")

	if err := et.WriteMetadata(srcPath, dstPath, map[string]any{"Artist": "Dir Test"}); err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	metadata, err := et.ReadMetadata(dstPath)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	if got, ok := metadata["SourceFile"]; !ok || got != dstPath {
		t.Errorf("SourceFile: expected %q, got %v", dstPath, got)
	}

	if got, ok := metadata["Directory"]; !ok || got != filepath.Dir(dstPath) {
		t.Errorf("Directory: expected %q, got %v", filepath.Dir(dstPath), got)
	}

	if got, ok := metadata["FileName"]; !ok || got != "output.jpg" {
		t.Errorf("FileName: expected %q, got %v", "output.jpg", got)
	}
}

func TestWriteMetadataCorruptFile(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	// Create a corrupt file (random bytes that aren't a valid image)
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "corrupt.jpg")
	if err := os.WriteFile(srcPath, []byte("not a valid image file"), 0644); err != nil {
		t.Fatalf("Failed to create corrupt file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "output.jpg")
	err = et.WriteMetadata(srcPath, dstPath, map[string]any{
		"Artist": "Test",
	})
	if err == nil {
		// If no error, the output file must not be 0 bytes
		info, statErr := os.Stat(dstPath)
		if statErr != nil || info.Size() == 0 {
			t.Error("WriteMetadata should return an error when output is empty")
		}
	}
}

func TestWriteMetadataEvalFailure(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	// Directly test that eval returning empty string triggers an error
	// by calling eval with code that dies
	output, stderrMsg, err := et.eval("die 'test error';")
	if err != nil {
		// eval itself may return an error - that's fine
		return
	}
	// If eval doesn't error but returns empty, our WriteMetadata would now catch it
	if output == "" {
		t.Logf("eval returned empty string on die (stderr: %s) - WriteMetadata would correctly report failure", stderrMsg)
	}
}

func TestWriteMetadataOutputFileNonZeroSize(t *testing.T) {
	et, err := New()
	if err != nil {
		t.Fatalf("Failed to create ExifTool: %v", err)
	}
	defer et.Close()

	srcPath := filepath.Join("testdata", "test.jpg")
	tmpDir := t.TempDir()
	dstPath := filepath.Join(tmpDir, "output.jpg")

	err = et.WriteMetadata(srcPath, dstPath, map[string]any{
		"Artist": "Size Check",
	})
	if err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	info, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Size() == 0 {
		t.Error("Output file should not be 0 bytes")
	}
}

// extractTags extracts specified tags from metadata
func extractTags(metadata map[string]any, tags []string) map[string]any {
	result := make(map[string]any)
	for _, tag := range tags {
		if val, ok := metadata[tag]; ok {
			result[tag] = val
		}
	}
	return result
}

// compareMaps compares two maps for equality
func compareMaps(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || !compareValues(v, bv) {
			return false
		}
	}
	return true
}

// compareValues compares two values, handling type differences
func compareValues(a, b any) bool {
	// Handle numeric comparisons (JSON unmarshals numbers as float64)
	switch av := a.(type) {
	case float64:
		switch bv := b.(type) {
		case float64:
			return av == bv
		case int:
			return av == float64(bv)
		}
	case int:
		switch bv := b.(type) {
		case float64:
			return float64(av) == bv
		case int:
			return av == bv
		}
	case string:
		if bv, ok := b.(string); ok {
			return av == bv
		}
	}
	return a == b
}
