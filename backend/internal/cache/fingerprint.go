package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var ErrNoFingerprintFilesFound = errors.New("no cache fingerprint files found")

func ComputeFingerprint(workspaceRoot string, fingerprintFiles []string) (string, []string, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return "", nil, errors.New("workspace root is required")
	}

	hasher := sha256.New()
	seen := make([]string, 0, len(fingerprintFiles))
	for _, rel := range fingerprintFiles {
		trimmed := strings.TrimSpace(rel)
		if trimmed == "" {
			continue
		}
		resolved, err := secureJoin(root, trimmed)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", nil, err
		}
		seen = append(seen, filepath.ToSlash(trimmed))
		_, _ = io.WriteString(hasher, filepath.ToSlash(trimmed))
		_, _ = io.WriteString(hasher, "\n")
		_, _ = hasher.Write(data)
		_, _ = io.WriteString(hasher, "\n")
	}

	if len(seen) == 0 {
		return "", nil, ErrNoFingerprintFilesFound
	}
	sort.Strings(seen)
	return hex.EncodeToString(hasher.Sum(nil)), seen, nil
}

func secureJoin(root string, rel string) (string, error) {
	cleanRel := filepath.Clean(strings.ReplaceAll(rel, "\\", "/"))
	if filepath.IsAbs(cleanRel) {
		return "", errors.New("absolute fingerprint path is not allowed")
	}
	if cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return "", errors.New("fingerprint path escapes workspace")
	}
	full := filepath.Join(root, cleanRel)
	normRoot := filepath.Clean(root)
	relPath, err := filepath.Rel(normRoot, full)
	if err != nil {
		return "", err
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", errors.New("fingerprint path escapes workspace")
	}
	return full, nil
}
