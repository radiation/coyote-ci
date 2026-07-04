package credentials

import (
	"errors"
	"strings"
	"sync"

	keyring "github.com/zalando/go-keyring"
)

var ErrNotFound = errors.New("credential not found")

type Store interface {
	Get(ref string) (string, error)
	Set(ref string, secret string) error
}

type KeyringStore struct {
	service string
}

func NewKeyringStore(service string) *KeyringStore {
	trimmed := strings.TrimSpace(service)
	if trimmed == "" {
		trimmed = "coyote-cli"
	}
	return &KeyringStore{service: trimmed}
}

func (s *KeyringStore) Get(ref string) (string, error) {
	secret, err := keyring.Get(s.service, strings.TrimSpace(ref))
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	return secret, nil
}

func (s *KeyringStore) Set(ref string, secret string) error {
	return keyring.Set(s.service, strings.TrimSpace(ref), secret)
}

type MemoryStore struct {
	mu      sync.Mutex
	secrets map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{secrets: map[string]string{}}
}

func (s *MemoryStore) Get(ref string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret, ok := s.secrets[strings.TrimSpace(ref)]
	if !ok {
		return "", ErrNotFound
	}
	return secret, nil
}

func (s *MemoryStore) Set(ref string, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets[strings.TrimSpace(ref)] = secret
	return nil
}
