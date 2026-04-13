package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrInvalidKeyInput = errors.New("invalid cache key input")

type KeyInput struct {
	Scope          string
	BuildID        string
	JobIdentity    string
	Image          string
	Platform       string
	Paths          []string
	KeyFilesDigest string
}

type keyEnvelope struct {
	Version        int      `json:"version"`
	Scope          string   `json:"scope"`
	BuildID        string   `json:"build_id,omitempty"`
	JobIdentity    string   `json:"job_identity,omitempty"`
	Image          string   `json:"image,omitempty"`
	Platform       string   `json:"platform,omitempty"`
	Paths          []string `json:"paths"`
	KeyFilesDigest string   `json:"key_files_digest"`
}

func ResolveKey(input KeyInput) (string, error) {
	scope := strings.TrimSpace(input.Scope)
	if scope == "" {
		return "", fmt.Errorf("%w: scope is required", ErrInvalidKeyInput)
	}
	if len(input.Paths) == 0 {
		return "", fmt.Errorf("%w: paths are required", ErrInvalidKeyInput)
	}
	if scope == "job" && strings.TrimSpace(input.JobIdentity) == "" {
		return "", fmt.Errorf("%w: job identity is required for job scope", ErrInvalidKeyInput)
	}
	if scope == "build" && strings.TrimSpace(input.BuildID) == "" {
		return "", fmt.Errorf("%w: build id is required for build scope", ErrInvalidKeyInput)
	}

	paths := append([]string(nil), input.Paths...)
	sort.Strings(paths)
	envelope := keyEnvelope{
		Version:        1,
		Scope:          scope,
		BuildID:        strings.TrimSpace(input.BuildID),
		JobIdentity:    strings.TrimSpace(input.JobIdentity),
		Image:          strings.TrimSpace(input.Image),
		Platform:       strings.TrimSpace(input.Platform),
		Paths:          paths,
		KeyFilesDigest: strings.TrimSpace(input.KeyFilesDigest),
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return fmt.Sprintf("v1/%s/%s", envelope.Scope, hex.EncodeToString(digest[:])), nil
}
