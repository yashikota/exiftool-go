# exiftool-go

WebAssemblyで動作するPure GoのExifToolラッパー

[zeroperl](https://github.com/6over3/zeroperl)（PerlをWebAssemblyにコンパイルしたもの）と[wazero](https://github.com/tetratelabs/wazero)（Pure GoのWebAssemblyランタイム）を使用して、外部依存なしでExifTool機能を提供します。

## 特徴

- **Pure Go**: CGO不要、クロスコンパイルが簡単
- **シングルバイナリ**: WebAssemblyモジュールが埋め込まれているため、1つのバイナリで配布可能
- **フル機能のExifTool**: 本物のExifTool（v13.42）を使用し、すべてのメタデータ形式（EXIF、IPTC、XMP、ICCなど）をサポート
- **外部依存なし**: システムにPerlやExifToolをインストールする必要なし

## CLI使用方法

```bash
go install github.com/yashikota/exiftool-go
```

```bash
# メタデータ読み取り
exiftool-go photo.jpg

# JSON出力
exiftool-go -json photo.jpg

# 複数ファイル
exiftool-go photo1.jpg photo2.jpg
```

## ライブラリ使用方法

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
    // ExifToolインスタンスを作成
    et, err := exiftool.New()
    if err != nil {
        log.Fatal(err)
    }
    defer et.Close()

    // 画像からメタデータを読み取り
    metadata, err := et.ReadMetadata("photo.jpg")
    if err != nil {
        log.Fatal(err)
    }

    // メタデータを表示
    for key, value := range metadata {
        fmt.Printf("%s: %v\n", key, value)
    }
}
```

## API

- `New() (*ExifTool, error)`  
    新しいExifToolインスタンスを作成します。使用後はCloseを呼び出してください。

- `NewWithContext(ctx context.Context) (*ExifTool, error)`  
    指定したコンテキストで新しいExifToolインスタンスを作成します。

- `(*ExifTool) Close() error`  
    ExifToolインスタンスに関連するすべてのリソースを解放します。

- `(*ExifTool) Version() (string, error)`  
    ExifToolのバージョン文字列を返します。

- `(*ExifTool) ReadMetadata(filePath string) (map[string]interface{}, error)`  
    画像ファイルからメタデータを読み取り、マップとして返します。

## 仕組み

1. **zeroperl**: Perl 5インタプリタをWASIサポート付きでWebAssemblyにコンパイル
2. **wazero**: Pure GoのWebAssemblyランタイムを提供
3. **ExifTool**: Perlモジュール（Image::ExifTool）がzeroperlのWebAssemblyバイナリに同梱
4. **このライブラリ**: すべてをクリーンなGo APIでラップ

## クレジット

- [ExifTool](https://exiftool.org/) by Phil Harvey
- [zeroperl](https://github.com/6over3/zeroperl) by 6over3
- [wazero](https://github.com/tetratelabs/wazero) by Tetrate
