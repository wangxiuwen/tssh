package runtime

import "github.com/wangxiuwen/tssh/internal/shared"

// callDecodeOutput lets runtime.go depend on shared.DecodeOutput without
// threading the import through the type file. Keeps the public surface
// area of internal/runtime trivially small while reusing shared's already-
// tested base64 + fallback logic.
func callDecodeOutput(s string) string { return shared.DecodeOutput(s) }
