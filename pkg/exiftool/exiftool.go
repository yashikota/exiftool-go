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
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed wasm/exiftool.wasm
var wasmFS embed.FS

//go:embed exiftool_cli.pl
var exiftoolCLI string

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

// TagInfo is one extracted metadata value with optional group information.
type TagInfo struct {
	Name  string
	Group string
	Value any
}

// ReadOptions controls metadata extraction.
type ReadOptions struct {
	Tags       []string
	Exclude    []string
	Duplicates bool
}

// New creates a new ExifTool instance.
func New() (*ExifTool, error) {
	return NewWithContext(context.Background())
}

// NewWithContext creates a new ExifTool instance with the given context.
func NewWithContext(ctx context.Context) (*ExifTool, error) {
	return newWithContext(ctx, false, nil, nil)
}

func newWithContext(ctx context.Context, rootFS bool, extraMounts []string, stdin io.Reader) (*ExifTool, error) {
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

	fsConfig := wazero.NewFSConfig()
	if rootFS {
		cwd, err := os.Getwd()
		if err != nil {
			et.Close()
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
		fsConfig = fsConfig.
			WithDirMount(cwd, "").
			WithDirMount(tmpDir, "/exiftool-go").
			WithDirMount(devDir, "/dev")
		for _, mount := range extraMounts {
			fsConfig = fsConfig.WithDirMount(mount, mount)
		}
	} else {
		fsConfig = fsConfig.
			WithDirMount(tmpDir, "/tmp").
			WithDirMount(devDir, "/dev")
	}

	// Configure module with WASI settings
	config := wazero.NewModuleConfig().
		WithStdout(et.stdout).
		WithStderr(et.stderr).
		WithArgs("perl").
		WithFSConfig(fsConfig)
	if stdin != nil {
		config = config.WithStdin(stdin)
	}

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

// RunCLI executes the bundled ExifTool command-line application with the given
// arguments. It returns stdout, stderr and the ExifTool exit status.
func RunCLI(args []string) (string, string, int, error) {
	return RunCLIWithContext(context.Background(), args)
}

// RunCLIWithContext executes the bundled ExifTool command-line application.
func RunCLIWithContext(ctx context.Context, args []string) (string, string, int, error) {
	return RunCLIWithStdin(ctx, args, nil)
}

// RunCLIWithStdin executes the bundled ExifTool command-line application with stdin.
func RunCLIWithStdin(ctx context.Context, args []string, stdin io.Reader) (string, string, int, error) {
	et, err := newWithContext(ctx, true, collectCLIMounts(args), stdin)
	if err != nil {
		return "", "", 1, err
	}
	defer et.Close()

	hostScriptPath := filepath.Join(et.tmpDir, "exiftool")
	const guestScriptPath = "/exiftool-go/exiftool"
	if err := os.WriteFile(hostScriptPath, []byte(exiftoolCLI), 0755); err != nil {
		return "", "", 1, fmt.Errorf("failed to write exiftool cli script: %w", err)
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return "", "", 1, fmt.Errorf("failed to marshal arguments: %w", err)
	}
	scriptJSON, err := json.Marshal(guestScriptPath)
	if err != nil {
		return "", "", 1, fmt.Errorf("failed to marshal script path: %w", err)
	}

	code := fmt.Sprintf(`
use JSON::PP;
my $json = JSON::PP->new->utf8;
my $args = $json->decode('%s');
my $script = $json->decode('%s');
@ARGV = @$args;
$0 = $script;
my $exit_code = 0;
{
    no warnings 'redefine';
    local *CORE::GLOBAL::exit = sub {
        my $code = shift;
        $code = 0 unless defined $code;
        die "__EXIFTOOL_GO_EXIT__$code\n";
    };
    my $ok = eval {
        my $result = do $script;
        die($@ || $!) unless defined $result;
        1;
    };
    if (!$ok) {
        my $err = $@;
        if ($err =~ /^__EXIFTOOL_GO_EXIT__(\d+)/) {
            $exit_code = $1;
        } else {
            print STDERR $err;
            $exit_code = 1;
        }
    }
}
eval { require IO::Handle; STDOUT->flush(); STDERR->flush(); };
print STDERR "\n__EXIFTOOL_GO_EXIT_CODE__=$exit_code\n";
`, string(argsJSON), string(scriptJSON))

	stdout, stderrMsg, err := et.eval(code)
	if err != nil {
		return stdout, stderrMsg, 1, err
	}

	exitCode := 0
	const marker = "__EXIFTOOL_GO_EXIT_CODE__="
	if idx := strings.LastIndex(stderrMsg, marker); idx >= 0 {
		codeStart := idx + len(marker)
		codeEnd := codeStart
		for codeEnd < len(stderrMsg) && stderrMsg[codeEnd] >= '0' && stderrMsg[codeEnd] <= '9' {
			codeEnd++
		}
		if parsed, parseErr := strconv.Atoi(stderrMsg[codeStart:codeEnd]); parseErr == nil {
			exitCode = parsed
		}
		stderrMsg = strings.TrimRight(stderrMsg[:idx], "\r\n")
	}

	return stdout, stderrMsg, exitCode, nil
}

func collectCLIMounts(args []string) []string {
	seen := make(map[string]bool)
	var mounts []string
	add := func(path string) {
		if !filepath.IsAbs(path) {
			return
		}
		mount := existingPathMount(path)
		if mount == "" || seen[mount] {
			return
		}
		seen[mount] = true
		mounts = append(mounts, mount)
	}

	for _, arg := range args {
		add(arg)
		if idx := strings.Index(arg, "<="); idx >= 0 {
			add(arg[idx+2:])
		}
		if idx := strings.Index(arg, "<"); idx >= 0 {
			add(arg[idx+1:])
		}
		if idx := strings.Index(arg, "="); idx >= 0 {
			add(arg[idx+1:])
		}
	}

	return mounts
}

func existingPathMount(path string) string {
	path = filepath.Clean(path)
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return path
		}
		return filepath.Dir(path)
	}
	for dir := filepath.Dir(path); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
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

// eval executes Perl code and returns stdout and stderr.
func (et *ExifTool) eval(code string) (string, string, error) {
	et.mu.Lock()
	defer et.mu.Unlock()

	et.stdout.Reset()
	et.stderr.Reset()

	// Write code to wasm memory
	codeBytes := append([]byte(code), 0)
	results, err := et.mallocFn.Call(et.ctx, uint64(len(codeBytes)))
	if err != nil {
		return "", "", fmt.Errorf("malloc failed: %w", err)
	}
	codePtr := uint32(results[0])
	defer et.freeFn.Call(et.ctx, uint64(codePtr))

	mem := et.mod.Memory()
	if !mem.Write(codePtr, codeBytes) {
		return "", "", fmt.Errorf("failed to write code to memory")
	}

	// Call eval
	_, err = et.callWithAsyncify(et.evalFn, uint64(codePtr), 0, 0, 0)
	if err != nil {
		return "", "", fmt.Errorf("eval failed: %w", err)
	}

	// Flush stdout
	if et.flushFn != nil {
		et.flushFn.Call(et.ctx)
	}

	return et.stdout.String(), et.stderr.String(), nil
}

// ReadMetadata reads metadata from an image file.
func (et *ExifTool) ReadMetadata(filePath string) (map[string]any, error) {
	tags, err := et.ReadMetadataTags(filePath, ReadOptions{})
	if err != nil {
		return nil, err
	}

	result := make(map[string]any, len(tags))
	for _, tag := range tags {
		result[tag.Name] = tag.Value
	}

	return result, nil
}

// ReadMetadataTags reads metadata from a file and returns tag information.
func (et *ExifTool) ReadMetadataTags(filePath string, opts ReadOptions) ([]TagInfo, error) {
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

	tagsJSON, err := json.Marshal(opts.Tags)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tags: %w", err)
	}
	excludeJSON, err := json.Marshal(opts.Exclude)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal excluded tags: %w", err)
	}
	duplicates := 0
	if opts.Duplicates {
		duplicates = 1
	}

	// Execute Perl code to extract metadata
	code := fmt.Sprintf(`
use Image::ExifTool;
use JSON::PP;
my $et = Image::ExifTool->new;
my $json = JSON::PP->new->utf8;
my $tags = $json->decode('%s');
my $exclude = $json->decode('%s');
my %%exclude;
foreach my $tag (@$exclude) {
    $tag =~ s/^[-]+//;
    $exclude{lc($tag)} = 1;
}
$et->Options(Duplicates => %d);
my $info = @$tags ? $et->ImageInfo('/tmp/input', @$tags) : $et->ImageInfo('/tmp/input');
my @result;
foreach my $tag (sort keys %%$info) {
    my $base = $tag;
    $base =~ s/ \(\d+\)$//;
    next if $exclude{lc($base)};
    my $val = $$info{$tag};
    my $out;
    if (ref($val) eq 'SCALAR') {
        $out = '[binary data]';
    } else {
        $out = $val;
    }
    push @result, {
        Name => $tag,
        Group => scalar($et->GetGroup($tag, 1)),
        Value => $out,
    };
}
print $json->encode(\@result);
`, string(tagsJSON), string(excludeJSON), duplicates)
	output, _, err := et.eval(code)
	if err != nil {
		return nil, err
	}

	var result []TagInfo
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w (output: %s)", err, output)
	}

	if len(opts.Tags) == 0 {
		result = replaceTagValue(result, "SourceFile", filePath)
		result = replaceTagValue(result, "Directory", filepath.Dir(filePath))
		result = replaceTagValue(result, "FileName", filepath.Base(filePath))

		// Fix FilePermissions from the real file (sandbox always reports ----------)
		if info, err := os.Stat(filePath); err == nil {
			result = replaceTagValue(result, "FilePermissions", formatFilePermissions(info.Mode()))
		}
	}

	return result, nil
}

func replaceTagValue(tags []TagInfo, name string, value any) []TagInfo {
	for i := range tags {
		if tags[i].Name == name {
			tags[i].Value = value
			return tags
		}
	}
	return append(tags, TagInfo{Name: name, Group: "File", Value: value})
}

// formatFilePermissions formats an os.FileMode into ExifTool's rwxrwxrwx style.
func formatFilePermissions(mode os.FileMode) string {
	const rwx = "rwxrwxrwx"
	buf := []byte("---------")
	for i := range 9 {
		if mode&(1<<uint(8-i)) != 0 {
			buf[i] = rwx[i]
		}
	}
	return string(buf)
}

// Version returns the ExifTool version.
func (et *ExifTool) Version() (string, error) {
	code := "use Image::ExifTool; print Image::ExifTool->VERSION;"
	output, _, err := et.eval(code)
	return output, err
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

	output, stderrMsg, err := et.eval(code)
	if err != nil {
		return fmt.Errorf("failed to execute write: %w", err)
	}

	// Check result: 1=success, 2=success with warnings, 0=failure
	// Empty output means Perl died (e.g. missing XS module) without producing a result.
	if output == "0" || output == "" {
		if stderrMsg != "" {
			return fmt.Errorf("exiftool write failed: %s", stderrMsg)
		}
		return fmt.Errorf("exiftool write failed")
	}

	// Read output file
	tmpOutput := et.tmpDir + "/output"
	outputData, err := os.ReadFile(tmpOutput)
	if err != nil {
		return fmt.Errorf("failed to read output file: %w", err)
	}
	defer os.Remove(tmpOutput)
	if len(outputData) == 0 {
		return fmt.Errorf("exiftool write produced empty output")
	}

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
