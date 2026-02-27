# erago

`erago` is a Go CLI/runtime for running Emuera-style `ERB/ERH/CSV` game files.

## Prerequisites

- Go `1.25+`

## Project Layout

- Put game files under a folder such as `era_files/<game_name>/`.
- Example included in this repo:
  - `era_files/mini_adventure`

## Quick Start (Run From Source)

```bash
go run ./cmd/erago -base ./era_files/mini_adventure -entry TITLE
```

## Build and Run Binary

Build:

```bash
go build -o ./bin/erago ./cmd/erago
```

Run:

```bash
./bin/erago -base ./era_files/mini_adventure -entry TITLE -savefmt json
```

Build + run in one command:

```bash
go build -o ./bin/erago ./cmd/erago && ./bin/erago -base ./era_files/mini_adventure -entry TITLE -savefmt json
```

## CLI Options

- `-base`: base directory containing `.erb/.erh/.csv` and save files
- `-dir`: deprecated alias for `-base`
- `-entry`: entry function name (default: `TITLE`)
- `-savefmt`: save payload format (`json`, `binary`, `both`)

See all options:

```bash
go run ./cmd/erago -h
```

## Save Files

- Save files are written under the same directory passed to `-base`.
- `SAVEVAR/SAVECHARA` support:
  - `json`: JSON payload in `.dat`
  - `binary`: Emuera-style binary `.dat`
  - `both`: binary `.dat` + companion `.json`

## TUI Controls

- `q`: quit
- `r`: rerun
- `j/k`, `pgup/pgdn`: scroll
- `g/G`: top/bottom
- `ctrl+c`: force quit

## Save Format Converter

Convert save data between JSON and binary:

```bash
go run ./cmd/savecodec -kind var -in ./save/var_case1.dat -out ./save/var_case1.bin.dat -to binary
go run ./cmd/savecodec -kind var -in ./save/var_case1.bin.dat -out ./save/var_case1.json -to json
```
