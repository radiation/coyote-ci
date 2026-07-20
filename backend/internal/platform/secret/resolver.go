package secret

import (
	"context"
	"errors"
	"os"
	"strings"
)

var ErrSecretRefRequired = errors.New("secret ref is required")
var ErrSecretNotFound = errors.New("secret is not configured")

type Resolver interface {
	Resolve(ctx context.Context, ref string) (string, error)
}

type EnvResolver struct {
	lookup func(string) (string, bool)
}

func NewEnvResolver() *EnvResolver {
	return &EnvResolver{lookup: os.LookupEnv}
}

func (r *EnvResolver) Resolve(_ context.Context, ref string) (string, error) {
	trimmedRef := strings.TrimSpace(ref)
	if trimmedRef == "" {
		return "", ErrSecretRefRequired
	}
	lookup := os.LookupEnv
	if r != nil && r.lookup != nil {
		lookup = r.lookup
	}
	value, ok := lookup(trimmedRef)
	trimmedValue := strings.TrimSpace(value)
	if !ok || trimmedValue == "" {
		return "", ErrSecretNotFound
	}
	return trimmedValue, nil
}
