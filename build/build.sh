#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT_DIR="$PROJECT_DIR/pkg/exiftool/wasm"

PERL_VERSION="${PERL_VERSION:-5.42.0}"
EXIFTOOL_VERSION="${EXIFTOOL_VERSION:-13.59}"

echo "Building exiftool.wasm..."
echo "  Perl version:    $PERL_VERSION"
echo "  ExifTool version: $EXIFTOOL_VERSION"

docker build \
    --build-arg PERL_VERSION="$PERL_VERSION" \
    --build-arg EXIFTOOL_VERSION="$EXIFTOOL_VERSION" \
    -t exiftool-wasm-builder \
    "$SCRIPT_DIR"

echo "Extracting exiftool.wasm..."
mkdir -p "$OUTPUT_DIR"
docker run --rm -v "$OUTPUT_DIR:/output" exiftool-wasm-builder

echo "Done: $OUTPUT_DIR/exiftool.wasm"
ls -lh "$OUTPUT_DIR/exiftool.wasm"
