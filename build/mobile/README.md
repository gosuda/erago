# mobile bridge

This package is for `gomobile bind`.

Exported API:

- `Run(filesJSON, entry, inputsJSON, saveFmt string) string`

JSON formats:

- `filesJSON`: `{ "ERB/MAIN.ERB": "...", "ERH/MAIN.ERH": "...", "CSV/GAMEBASE.CSV": "..." }`
- `inputsJSON`: `["1", "hello"]`

Return JSON:

- `{ "outputs": [{"Text":"...","NewLine":true}], "error": "..." }`
