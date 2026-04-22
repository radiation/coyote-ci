package api

import "encoding/json"

type CreateSourceCredentialRequest struct {
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Username  *string `json:"username,omitempty"`
	SecretRef string  `json:"secret_ref"`
}

type UpdateSourceCredentialRequest struct {
	Name      *string     `json:"name,omitempty"`
	Kind      *string     `json:"kind,omitempty"`
	Username  StringPatch `json:"username,omitempty"`
	SecretRef *string     `json:"secret_ref,omitempty"`
}

// StringPatch represents a tri-state string update field for PATCH/PUT-style
// request decoding:
// - Set=false: field omitted
// - Set=true, Value=nil: field explicitly set to null
// - Set=true, Value!=nil: field set to a concrete string value
type StringPatch struct {
	Set   bool
	Value *string
}

func (p *StringPatch) UnmarshalJSON(data []byte) error {
	p.Set = true
	if string(data) == "null" {
		p.Value = nil
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	p.Value = &value
	return nil
}

type SourceCredentialResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Username  *string `json:"username,omitempty"`
	SecretRef string  `json:"secret_ref"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type SourceCredentialListResponse struct {
	Credentials []SourceCredentialResponse `json:"credentials"`
}

type SourceCredentialEnvelope struct {
	Data SourceCredentialResponse `json:"data"`
}

type SourceCredentialListEnvelope struct {
	Data SourceCredentialListResponse `json:"data"`
}
