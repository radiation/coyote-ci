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

	for _, seg := range strings.Split(trimmed, "/") {
		if seg == ".." {
			return ErrInvalidStorageKey
		}
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." {
		return ErrInvalidStorageKey
	}

	return nil
}
