# erago

![erago](./image.png)

`erago` is a Go runtime/CLI for Emuera-style game files (`.erb`, `.erh`, `.csv`).

## Prerequisites

- Go `1.25+`

## Quick Start

Run directly from source:

```bash
go run ./cmd/erago -base ./era_files/1_adventure -entry TITLE
```

## Build Directory

All build/deploy source assets are under `build/`:

- `build/web/index.html`: web runner UI
- `build/web/main_js.go`: wasm entry (`js/wasm`)
- `build/mobile/bridge.go`: gomobile bridge API

Use `Makefile` for build/release commands.

## Desktop + Web Build

Build Linux/macOS/Windows binaries and web wasm bundle:

```bash
make build
```

Output artifacts:

- `dist/linux-amd64/erago`
- `dist/linux-arm64/erago`
- `dist/darwin-amd64/erago`
- `dist/darwin-arm64/erago`
- `dist/windows-amd64/erago.exe`
- `dist/windows-arm64/erago.exe`
- `dist/web/index.html`
- `dist/web/wasm_exec.js`
- `dist/web/erago.wasm`

Run desktop binary:

```bash
./dist/linux-amd64/erago -base ./era_files/2_dialog_tool -entry TITLE
```

Run web build locally:

```bash
make serve-web
```

In the browser:
- choose a game folder that contains `.erb/.erh/.csv`
- add expected `INPUT` values to the queue
- click `Run`

## Mobile Build

Install gomobile (one-time):

```bash
go install golang.org/x/mobile/cmd/gomobile@latest
```

Build Android/iOS packages:

```bash
make mobile
```

Output artifacts:

- `dist/mobile/erago_mobile.aar` (Android)
- `dist/mobile/EragoMobile.xcframework` (iOS)

## CLI Options

- `-base`: base directory containing game files and save files
- `-dir`: deprecated alias of `-base`
- `-entry`: entry function (default: `TITLE`)

Show help:

```bash
go run ./cmd/erago -h
```

## Save Files

- Save files are written to the same path as `-base`.
- `cmd/erago` writes `SAVEVAR/SAVECHARA` as Emuera-style binary `.dat`.
