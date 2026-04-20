package managedimage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var dependencyFingerprintCandidates = []string{
	"backend/go.mod",
	"backend/go.sum",
	"frontend/package-lock.json",
	"frontend/pnpm-lock.yaml",
	"frontend/yarn.lock",
	"frontend/bun.lockb",
	"backend/Dockerfile",
	"frontend/Dockerfile",
}

func ComputeDependencyFingerprint(repoRoot string, pipelinePath string) (string, []string, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return "", nil, fmt.Errorf("repo root is required")
	}

	paths := append([]string{}, dependencyFingerprintCandidates...)
	if strings.TrimSpace(pipelinePath) != "" {
		paths = append(paths, filepath.ToSlash(filepath.Clean(strings.TrimSpace(pipelinePath))))
	}
	sort.Strings(paths)

	h := sha256.New()
	included := make([]string, 0, len(paths))
	for _, rel := range paths {
		cleanRel := filepath.ToSlash(filepath.Clean(rel))
		if cleanRel == "." || strings.HasPrefix(cleanRel, "../") {
			continue
		}
		full := filepath.Join(repoRoot, filepath.FromSlash(cleanRel))
		content, err := os.ReadFile(full)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", nil, err
		}
		included = append(included, cleanRel)
		h.Write([]byte(cleanRel))
		h.Write([]byte("\n"))
		h.Write(content)
		h.Write([]byte("\n"))
	}

	return hex.EncodeToString(h.Sum(nil)), included, nil
}
