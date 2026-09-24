# exiftool-go

Pure Go ExifTool wrapper powered by WebAssembly.

Uses [zeroperl](https://github.com/6over3/zeroperl) (Perl compiled to WebAssembly) and [wazero](https://github.com/tetratelabs/wazero) (pure Go WebAssembly runtime) to provide ExifTool functionality without any external dependencies.

<img width="1915" height="910" alt="exiftool-go" src="https://github.com/user-attachments/assets/858cac94-806a-4acb-9145-bb1b0a99cd39" />

## Features


- **Pure Go**: No CGO required, easy cross-compilation
- **Single Binary**: WebAssembly module is embedded, distributable as a single binary
- **Full ExifTool**: Uses the real ExifTool (v13.42), supporting all metadata formats (EXIF, IPTC, XMP, ICC, etc.)
- **No External Dependencies**: No need to install Perl or ExifTool on the system

## CLI Usage

Download binary from [Releases](https://github.com/yashikota/exiftool-go/releases)  

or  

```bash
go install github.com/yashikota/exiftool-go@latest
```

```bash
# Read metadata
exiftool-go photo.jpg

# JSON output
exiftool-go -json photo.jpg
exiftool-go -j photo.jpg

# Multiple files
exiftool-go photo1.jpg photo2.jpg

# ExifTool-compatible read options
exiftool-go -ver
exiftool-go -s -FileType photo.jpg
exiftool-go -G -FileType photo.jpg
exiftool-go -a photo.jpg

# Write PDF metadata in place
exiftool-go -Author="Test Author" -Title="Test Title" document.pdf

# Write PDF metadata in place without creating a _original backup
exiftool-go -overwrite_original -Author="Test Author" document.pdf

# Write PDF metadata to a new file
exiftool-go -o output.pdf -Author="Test Author" -Title="Test Title" document.pdf
```

The CLI delegates to the bundled ExifTool application script, so ExifTool command-line options such as `-tagsFromFile`, `-r`, `-ext`, `-if`, `-execute`, `-stay_open`, `-TAG`, `--TAG`, and `-TAG=value` are handled by ExifTool itself.

## Library Usage

```sh
go get github.com/yashikota/exiftool-go
```

```go
package main

import (
    "fmt"
    "log"

    "github.com/yashikota/exiftool-go/pkg/exiftool"
)

func main() {
    // Create ExifTool instance
    et, err := exiftool.New()
    if err != nil {
        log.Fatal(err)
    }
    defer et.Close()

    // Read metadata from image
    metadata, err := et.ReadMetadata("photo.jpg")
    if err != nil {
        log.Fatal(err)
    }

    // Print metadata
    for key, value := range metadata {
        fmt.Printf("%s: %v\n", key, value)
    }
}
```

## API

- `New() (*ExifTool, error)`

    Creates a new ExifTool instance. Call Close when done.

- `NewWithContext(ctx context.Context) (*ExifTool, error)`

    Creates a new ExifTool instance with the given context.

- `(*ExifTool) Close() error`

    Releases all resources associated with the ExifTool instance.

- `(*ExifTool) Version() (string, error)`

    Returns the ExifTool version string.

- `(*ExifTool) ReadMetadata(filePath string) (map[string]any, error)`

    Reads metadata from an image file and returns it as a map.

- `(*ExifTool) WriteMetadata(srcPath string, dstPath string, tags map[string]any) error`

    Writes multiple tags to an image file. If dstPath is empty, the source file is modified in place.

- `(*ExifTool) SetTag(srcPath string, dstPath string, tag string, value string) error`

    Writes a single tag to an image file. If dstPath is empty, the source file is modified in place.

## How It Works

1. **zeroperl**: Compiles Perl 5 interpreter to WebAssembly with WASI support
2. **wazero**: Provides a pure Go WebAssembly runtime
3. **ExifTool**: Perl module (Image::ExifTool) is bundled in zeroperl's WebAssembly binary
4. **This library**: Wraps everything in a clean Go API

## Credits

- [ExifTool](https://exiftool.org/) by Phil Harvey
- [zeroperl](https://github.com/6over3/zeroperl) by 6over3
- [wazero](https://github.com/tetratelabs/wazero) by Tetrate
