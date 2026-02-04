# exiftool-go

Pure Go ExifTool wrapper powered by WebAssembly.

Uses [zeroperl](https://github.com/6over3/zeroperl) (Perl compiled to WebAssembly) and [wazero](https://github.com/tetratelabs/wazero) (pure Go WebAssembly runtime) to provide ExifTool functionality without any external dependencies.

## Features

- **Pure Go**: No CGO required, easy cross-compilation
- **Single Binary**: WebAssembly module is embedded, distributable as a single binary
- **Full ExifTool**: Uses the real ExifTool (v13.42), supporting all metadata formats (EXIF, IPTC, XMP, ICC, etc.)
- **No External Dependencies**: No need to install Perl or ExifTool on the system

## CLI Usage

```bash
go install github.com/yashikota/exiftool-go
```

```bash
# Read metadata
exiftool-go photo.jpg

# JSON output
exiftool-go -json photo.jpg

# Multiple files
exiftool-go photo1.jpg photo2.jpg
```

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

- `(*ExifTool) ReadMetadata(filePath string) (map[string]interface{}, error)`  
    Reads metadata from an image file and returns it as a map.

## How It Works

1. **zeroperl**: Compiles Perl 5 interpreter to WebAssembly with WASI support
2. **wazero**: Provides a pure Go WebAssembly runtime
3. **ExifTool**: Perl module (Image::ExifTool) is bundled in zeroperl's WebAssembly binary
4. **This library**: Wraps everything in a clean Go API

## Credits

- [ExifTool](https://exiftool.org/) by Phil Harvey
- [zeroperl](https://github.com/6over3/zeroperl) by 6over3
- [wazero](https://github.com/tetratelabs/wazero) by Tetrate
