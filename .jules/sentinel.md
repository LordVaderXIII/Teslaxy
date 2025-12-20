# Sentinel Journal

## 2025-12-11 - Critical Path Traversal Vulnerability
**Vulnerability:** The `/api/video/*path` endpoint allowed arbitrary file reads because it directly used the path parameter in `c.File()` without checking if it was within the intended directory.
**Learning:** `gin.Context.File()` does not automatically sanitize or restrict paths to a specific root. It trusts the input path.
**Prevention:** Always use `filepath.Clean()` and `filepath.Rel()` to ensure the resolved path starts with the intended root directory before serving files.

## 2025-12-14 - Manual JSON Construction in Go
**Vulnerability:** The application was constructing JWT JSON payloads using `fmt.Sprintf`, which allows for JSON injection if the inputs contain unescaped characters (like quotes).
**Learning:** Even in strongly typed languages like Go, manual string concatenation for structured data (JSON, SQL, HTML) is dangerous. Developers might assume simple inputs are safe, but it breaks the contract of the data format.
**Prevention:** Always use the standard library's marshaling functions (`json.Marshal`) or strict structs to generate JSON. Never treat JSON generation as a string formatting problem.

## 2025-12-16 - Hardcoded JWT Secret Default
**Vulnerability:** The application fell back to a hardcoded string ("default-secret-key-change-me") when the `JWT_SECRET` environment variable was missing, allowing attackers to forge tokens on instances with default configuration.
**Learning:** Default values for security-critical parameters (secrets, passwords) often become production vulnerabilities because users forget to override them. "Secure by default" means failing or generating a secure random value, not using a weak constant.
**Prevention:** Use `crypto/rand` to generate ephemeral secrets if not provided, or panic on startup. Log a loud warning when falling back to generated secrets.

## 2025-12-18 - Insecure Default Password
**Vulnerability:** The application used a hardcoded default password ("tesla") for the "admin" user when the `ADMIN_PASS` environment variable was not set, making unconfigured instances trivially exploitable.
**Learning:** Hardcoded fallbacks for authentication credentials defeat the purpose of authentication. Users often deploy with defaults for testing and forget to change them.
**Prevention:** Implement "Secure by Default": if `ADMIN_PASS` is missing, generate a strong random password at startup and log it. This ensures security without breaking the "zero-config" usability goal.

## 2025-12-20 - Unbounded In-Memory Rate Limiting
**Vulnerability:** Implementing simple map-based rate limiting without cleanup created a memory leak risk (DoS) where attackers could exhaust server RAM by spoofing many unique IP addresses.
**Learning:** Security controls themselves can become vulnerabilities if they consume unbounded resources. "Simple" in-memory solutions often lack the eviction logic found in mature libraries (like Redis or proper LRU caches).
**Prevention:** When implementing custom stateful security controls (like rate limiters), always include a mechanism to prune old data (e.g., a background cleanup goroutine or usage of `golang.org/x/time/rate` with a wrapper).
