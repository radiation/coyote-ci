package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvServer  = "COYOTE_SERVER"
	EnvToken   = "COYOTE_TOKEN"
	EnvContext = "COYOTE_CONTEXT"
)

type File struct {
	CurrentContext string             `json:"current_context,omitempty"`
	Contexts       map[string]Context `json:"contexts,omitempty"`
}

type Context struct {
	Name          string `json:"name"`
	ServerURL     string `json:"server_url"`
	CredentialRef string `json:"credential_ref,omitempty"`
	DefaultOutput string `json:"default_output,omitempty"`
}

type Store struct {
	configPath    string
	userConfigDir func() (string, error)
	replaceFile   func(string, string) error
}

func NewStore(configPath string) *Store {
	return &Store{configPath: strings.TrimSpace(configPath), userConfigDir: os.UserConfigDir, replaceFile: replaceFileAtomic}
}

func (s *Store) Path() (string, error) {
	if strings.TrimSpace(s.configPath) != "" {
		return s.configPath, nil
	}
	baseDir, err := s.userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, "coyote", "config.json"), nil
}

func (s *Store) Load() (File, error) {
	path, err := s.Path()
	if err != nil {
		return File{}, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return File{Contexts: map[string]Context{}}, nil
		}
		return File{}, err
	}
	var cfg File
	if err := json.Unmarshal(body, &cfg); err != nil {
		return File{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]Context{}
	}
	for name, ctx := range cfg.Contexts {
		if strings.TrimSpace(ctx.Name) == "" {
			ctx.Name = name
			cfg.Contexts[name] = ctx
		}
	}
	return cfg, nil
}

func (s *Store) Save(cfg File) error {
	path, err := s.Path()
	if err != nil {
		return err
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]Context{}
	}
	for name, ctx := range cfg.Contexts {
		ctx.Name = name
		cfg.Contexts[name] = ctx
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	body = append(body, '\n')

	dir := filepath.Dir(path)
	mkdirErr := os.MkdirAll(dir, 0o700)
	if mkdirErr != nil {
		return mkdirErr
	}
	tmpFile, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	cleanup := true
	defer func() {
		_ = tmpFile.Close()
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	err = tmpFile.Chmod(0o600)
	if err != nil {
		return err
	}
	_, err = tmpFile.Write(body)
	if err != nil {
		return err
	}
	err = tmpFile.Close()
	if err != nil {
		return err
	}
	err = s.replaceFile(tmpName, path)
	if err != nil {
		return err
	}
	cleanup = false
	return nil
}

func NormalizeServerURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("server url is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse server url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("server url must use http or https")
	}
	if parsed.Fragment != "" {
		return "", errors.New("server url must not include a fragment")
	}
	if parsed.User != nil {
		return "", errors.New("server url must not include embedded credentials")
	}
	if parsed.Host == "" {
		return "", errors.New("server url must include host")
	}
	if parsed.RawQuery != "" {
		return "", errors.New("server url must not include a query")
	}
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if path == "." || path == "/" {
		path = ""
	}
	parsed.Path = path
	parsed.RawPath = ""
	return parsed.String(), nil
}

func NormalizeContextName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("context name is required")
	}
	return name, nil
}

func NormalizeOutput(raw string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	switch trimmed {
	case "", "human", "json":
		return trimmed, nil
	default:
		return "", errors.New("output must be human or json")
	}
}
