package artifact

import (
	"path"
	"strings"
)

// validateKey checks that an artifact storage key is safe for use across all
// storage backends. It rejects empty keys, backslashes, absolute paths, and
// path-traversal attempts ("..").
func validateKey(key string) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ErrInvalidStorageKey
	}
	if strings.Contains(trimmed, "\\") {
		return ErrInvalidStorageKey
	}
	if strings.HasPrefix(trimmed, "/") {
		return ErrInvalidStorageKey
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ErrInvalidStorageKey
	}

	return nil
}
