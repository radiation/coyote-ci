package domain

import "time"

type SourceCredentialKind string

const (
	SourceCredentialKindHTTPSToken SourceCredentialKind = "https_token"
	SourceCredentialKindSSHKey     SourceCredentialKind = "ssh_key"
)

type SourceCredential struct {
	ID        string
	Name      string
	Kind      SourceCredentialKind
	Username  *string
	SecretRef string
	CreatedAt time.Time
	UpdatedAt time.Time
}
