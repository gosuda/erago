# era_files

- `era_files/ERB` : `.erb`
- `era_files/ERH` : `.erh`
- `era_files/CSV` : `.csv`

```bash
go run ./cmd/erago -dir ./era_files -entry TITLE -savefmt json
```

## Run 1_adventure:
```bash
go run ./cmd/erago -dir ./era_files/1_adventure -entry TITLE -savefmt json
```