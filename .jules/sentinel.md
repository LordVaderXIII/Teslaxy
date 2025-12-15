# Sentinel Journal

## 2025-12-11 - Critical Path Traversal Vulnerability
**Vulnerability:** The `/api/video/*path` endpoint allowed arbitrary file reads because it directly used the path parameter in `c.File()` without checking if it was within the intended directory.
**Learning:** `gin.Context.File()` does not automatically sanitize or restrict paths to a specific root. It trusts the input path.
**Prevention:** Always use `filepath.Clean()` and `filepath.Rel()` to ensure the resolved path starts with the intended root directory before serving files.

## 2025-12-14 - Manual JSON Construction in Go
**Vulnerability:** The application was constructing JWT JSON payloads using `fmt.Sprintf`, which allows for JSON injection if the inputs contain unescaped characters (like quotes).
**Learning:** Even in strongly typed languages like Go, manual string concatenation for structured data (JSON, SQL, HTML) is dangerous. Developers might assume simple inputs are safe, but it breaks the contract of the data format.
**Prevention:** Always use the standard library's marshaling functions (`json.Marshal`) or strict structs to generate JSON. Never treat JSON generation as a string formatting problem.

## 2025-12-15 - Secure Defaults for Secrets
**Vulnerability:** The application used a hardcoded fallback string ("default-secret-key-change-me") when `JWT_SECRET` was not configured. This meant all unconfigured instances shared the same secret, allowing attackers to forge tokens.
**Learning:** Hardcoded "change me" defaults are often left unchanged in production. Security must be enabled by default.
**Prevention:** If a secret is missing, generate a cryptographically strong random one at runtime. This provides security (unique per instance) at the cost of session persistence across restarts, which is a safe tradeoff.
