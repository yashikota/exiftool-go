// Package exiftool provides a pure Go wrapper around ExifTool via WebAssembly.
// It uses zeroperl (Perl compiled to WebAssembly) and wazero (pure Go wasm runtime)
// to provide ExifTool functionality without any external dependencies.
package exiftool

import (
	"bytes"
	"context"
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed wasm/exiftool.wasm
var wasmFS embed.FS

const (
	// asyncify constants
	dataAddr  = 16
	dataStart = 24
	dataEnd   = 1024 * 1024 // 1MB
)

// ExifTool represents an ExifTool instance backed by WebAssembly.
type ExifTool struct {
	mu      sync.Mutex
	ctx     context.Context
	runtime wazero.Runtime
	mod     api.Module
	stdout  *bytes.Buffer
	stderr  *bytes.Buffer
	tmpDir  string
	devDir  string

	// cached functions
	mallocFn    api.Function
	freeFn      api.Function
	evalFn      api.Function
	flushFn     api.Function
	getState    api.Function
	stopUnwind  api.Function
	startRewind api.Function
	stopRewind  api.Function
}

// New creates a new ExifTool instance.
func New() (*ExifTool, error) {
	return NewWithContext(context.Background())
}

// NewWithContext creates a new ExifTool instance with the given context.
func NewWithContext(ctx context.Context) (*ExifTool, error) {
	// Load wasm binary
	wasmBytes, err := wasmFS.ReadFile("wasm/exiftool.wasm")
	if err != nil {
		return nil, fmt.Errorf("failed to read wasm: %w", err)
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "exiftool-go-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Create dummy /dev/null for WASI compatibility
	devDir := tmpDir + "/dev"
	if err := os.MkdirAll(devDir, 0755); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to create dev dir: %w", err)
	}
	if err := os.WriteFile(devDir+"/null", []byte{}, 0644); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to create /dev/null: %w", err)
	}

	et := &ExifTool{
		ctx:    ctx,
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		tmpDir: tmpDir,
		devDir: devDir,
	}

	// Create wazero runtime
	et.runtime = wazero.NewRuntime(ctx)

	// Instantiate WASI snapshot preview1
	wasi_snapshot_preview1.MustInstantiate(ctx, et.runtime)

	// Create env module for host function callback
	_, err = et.runtime.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, funcId, argPtr, argLen uint32) uint32 {
			return 0
		}).
		Export("call_host_function").
		Instantiate(ctx)
	if err != nil {
		et.Close()
		return nil, fmt.Errorf("failed to create env module: %w", err)
	}

	// Configure module with WASI settings
	config := wazero.NewModuleConfig().
		WithStdout(et.stdout).
		WithStderr(et.stderr).
		WithArgs("perl").
		WithFSConfig(wazero.NewFSConfig().
			WithDirMount(tmpDir, "/tmp").
			WithDirMount(devDir, "/dev"))

	// Instantiate module
	et.mod, err = et.runtime.InstantiateWithConfig(ctx, wasmBytes, config)
	if err != nil {
		et.Close()
		return nil, fmt.Errorf("failed to instantiate module: %w", err)
	}

	// Setup asyncify data buffer
	mem := et.mod.Memory()
	dataBuffer := make([]byte, 8)
	binary.LittleEndian.PutUint32(dataBuffer[0:4], dataStart)
	binary.LittleEndian.PutUint32(dataBuffer[4:8], dataEnd)
	if !mem.Write(dataAddr, dataBuffer) {
		et.Close()
		return nil, fmt.Errorf("failed to write asyncify data buffer")
	}

	// Cache exported functions
	et.mallocFn = et.mod.ExportedFunction("malloc")
	et.freeFn = et.mod.ExportedFunction("free")
	et.evalFn = et.mod.ExportedFunction("zeroperl_eval")
	et.flushFn = et.mod.ExportedFunction("zeroperl_flush")
	et.getState = et.mod.ExportedFunction("asyncify_get_state")
	et.stopUnwind = et.mod.ExportedFunction("asyncify_stop_unwind")
	et.startRewind = et.mod.ExportedFunction("asyncify_start_rewind")
	et.stopRewind = et.mod.ExportedFunction("asyncify_stop_rewind")

	// Call _initialize
	if initFn := et.mod.ExportedFunction("_initialize"); initFn != nil {
		if _, err := initFn.Call(ctx); err != nil {
			et.Close()
			return nil, fmt.Errorf("_initialize failed: %w", err)
		}
	}

	// Call zeroperl_init to initialize Perl interpreter
	if perlInitFn := et.mod.ExportedFunction("zeroperl_init"); perlInitFn != nil {
		if _, err := et.callWithAsyncify(perlInitFn); err != nil {
			et.Close()
			return nil, fmt.Errorf("zeroperl_init failed: %w", err)
		}
	}

	return et, nil
}

// Close releases all resources.
func (et *ExifTool) Close() error {
	if et.mod != nil {
		et.mod.Close(et.ctx)
	}
	if et.runtime != nil {
		et.runtime.Close(et.ctx)
	}
	if et.tmpDir != "" {
		os.RemoveAll(et.tmpDir)
	}
	return nil
}

// callWithAsyncify wraps a function call with asyncify support.
func (et *ExifTool) callWithAsyncify(fn api.Function, args ...uint64) ([]uint64, error) {
	mem := et.mod.Memory()
	dataBuffer := make([]byte, 8)

	for {
		results, err := fn.Call(et.ctx, args...)
		if err != nil {
			return nil, err
		}

		stateResults, _ := et.getState.Call(et.ctx)
		state := uint32(stateResults[0])

		switch state {
		case 0: // NORMAL
			return results, nil
		case 1: // UNWINDING
			et.stopUnwind.Call(et.ctx)
			binary.LittleEndian.PutUint32(dataBuffer[0:4], dataStart)
			binary.LittleEndian.PutUint32(dataBuffer[4:8], dataEnd)
			mem.Write(dataAddr, dataBuffer)
			et.startRewind.Call(et.ctx, dataAddr)
		case 2: // REWINDING
			et.stopRewind.Call(et.ctx)
			return results, nil
		}
	}
}

// eval executes Perl code and returns stdout.
func (et *ExifTool) eval(code string) (string, error) {
	et.mu.Lock()
	defer et.mu.Unlock()

	et.stdout.Reset()
	et.stderr.Reset()

	// Write code to wasm memory
	codeBytes := append([]byte(code), 0)
	results, err := et.mallocFn.Call(et.ctx, uint64(len(codeBytes)))
	if err != nil {
		return "", fmt.Errorf("malloc failed: %w", err)
	}
	codePtr := uint32(results[0])
	defer et.freeFn.Call(et.ctx, uint64(codePtr))

	mem := et.mod.Memory()
	if !mem.Write(codePtr, codeBytes) {
		return "", fmt.Errorf("failed to write code to memory")
	}

	// Call eval
	_, err = et.callWithAsyncify(et.evalFn, uint64(codePtr), 0, 0, 0)
	if err != nil {
		return "", fmt.Errorf("eval failed: %w", err)
	}

	// Flush stdout
	if et.flushFn != nil {
		et.flushFn.Call(et.ctx)
	}

	return et.stdout.String(), nil
}

// ReadMetadata reads metadata from an image file.
func (et *ExifTool) ReadMetadata(filePath string) (map[string]any, error) {
	// Copy file to temp directory for WASI access
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	tmpFile := et.tmpDir + "/input"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	defer os.Remove(tmpFile)

	// Execute Perl code to extract metadata
	code := `
use Image::ExifTool;
use JSON::PP;
my $et = Image::ExifTool->new;
my $info = $et->ImageInfo('/tmp/input');
my %result;
foreach my $tag (keys %$info) {
    my $val = $$info{$tag};
    if (ref($val) eq 'SCALAR') {
        $result{$tag} = '[binary data]';
    } else {
        $result{$tag} = $val;
    }
}
print JSON::PP->new->utf8->encode(\%result);
`
	output, err := et.eval(code)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w (output: %s)", err, output)
	}

	return result, nil
}

// Version returns the ExifTool version.
func (et *ExifTool) Version() (string, error) {
	code := "use Image::ExifTool; print Image::ExifTool->VERSION;"
	return et.eval(code)
}

// WriteMetadata writes multiple tags to an image file.
// If dstPath is empty, the source file is modified in place.
func (et *ExifTool) WriteMetadata(srcPath string, dstPath string, tags map[string]any) error {
	// Read source file
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read source file: %w", err)
	}

	// Write to temp input file
	tmpInput := et.tmpDir + "/input"
	if err := os.WriteFile(tmpInput, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp input file: %w", err)
	}
	defer os.Remove(tmpInput)

	// Convert tags to JSON
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	// Execute Perl code to write metadata
	code := fmt.Sprintf(`
use Image::ExifTool;
use JSON::PP;
my $et = Image::ExifTool->new;
my $tags = JSON::PP->new->utf8->decode('%s');
foreach my $tag (keys %%$tags) {
    $et->SetNewValue($tag, $tags->{$tag});
}
my $result = $et->WriteInfo('/tmp/input', '/tmp/output');
print $result;
`, string(tagsJSON))

	output, err := et.eval(code)
	if err != nil {
		return fmt.Errorf("failed to execute write: %w", err)
	}

	// Check result: 1=success, 2=success with warnings, 0=failure
	if output == "0" {
		return fmt.Errorf("exiftool write failed")
	}

	// Read output file
	tmpOutput := et.tmpDir + "/output"
	outputData, err := os.ReadFile(tmpOutput)
	if err != nil {
		return fmt.Errorf("failed to read output file: %w", err)
	}
	defer os.Remove(tmpOutput)

	// Determine destination path
	dest := dstPath
	if dest == "" {
		dest = srcPath
	}

	// Write to destination
	if err := os.WriteFile(dest, outputData, 0644); err != nil {
		return fmt.Errorf("failed to write destination file: %w", err)
	}

	return nil
}

// SetTag writes a single tag to an image file.
// If dstPath is empty, the source file is modified in place.
func (et *ExifTool) SetTag(srcPath string, dstPath string, tag string, value string) error {
	return et.WriteMetadata(srcPath, dstPath, map[string]any{tag: value})
}
