## 2025-10-29 - JSON Payload Optimization (Implemented)
**Learning:** Storing JSON as `string` in Go structs (`string` field with `json` tag) causes double-encoding when marshaled (e.g., `"{\"key\":\"val\"}"`), increasing payload size and requiring `JSON.parse` on the frontend.
**Action:** Use `json.RawMessage` for such fields. This embeds the raw JSON bytes directly into the output (e.g., `{"key":"val"}`), reducing size (no escaping) and CPU overhead (no frontend parsing). Verified with GORM/SQLite that `json.RawMessage` correctly maps to TEXT columns.
