package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

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
	paths := append([]string(nil), input.Paths...)
	sort.Strings(paths)
	envelope := keyEnvelope{
		Version:        1,
		Scope:          strings.TrimSpace(input.Scope),
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
