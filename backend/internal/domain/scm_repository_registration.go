package domain

import (
	"fmt"
	"strings"
	"time"
)

const maxSCMRepositoryTextLen = 300

type SCMRepositoryRegistration struct {
	ID                   string
	ConnectionID         string
	ProviderRepositoryID string
	Owner                string
	Name                 string
	FullName             string
	CloneURL             string
	WebURL               string
	DefaultBranch        *string
	Archived             bool
	Disabled             bool
	MetadataRefreshedAt  time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (r SCMRepositoryRegistration) Normalize() SCMRepositoryRegistration {
	r.ID = strings.TrimSpace(r.ID)
	r.ConnectionID = strings.TrimSpace(r.ConnectionID)
	r.ProviderRepositoryID = strings.TrimSpace(r.ProviderRepositoryID)
	r.Owner = truncateDomainText(strings.TrimSpace(r.Owner), maxSCMRepositoryTextLen)
	r.Name = truncateDomainText(strings.TrimSpace(r.Name), maxSCMRepositoryTextLen)
	r.FullName = truncateDomainText(strings.TrimSpace(r.FullName), maxSCMRepositoryTextLen)
	r.CloneURL = normalizeSCMBaseURL("", "", r.CloneURL, false)
	r.WebURL = normalizeSCMBaseURL("", "", r.WebURL, false)
	r.DefaultBranch = trimAndTruncateOptionalString(r.DefaultBranch, maxSCMRepositoryTextLen)
	r.MetadataRefreshedAt = r.MetadataRefreshedAt.UTC()
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	return r
}

func (r SCMRepositoryRegistration) Validate() error {
	r = r.Normalize()
	if r.ID == "" {
		return fmt.Errorf("scm registered repository id is required")
	}
	if r.ConnectionID == "" {
		return fmt.Errorf("scm registered repository connection id is required")
	}
	if r.ProviderRepositoryID == "" {
		return fmt.Errorf("scm registered repository provider repository id is required")
	}
	if r.Owner == "" {
		return fmt.Errorf("scm registered repository owner is required")
	}
	if r.Name == "" {
		return fmt.Errorf("scm registered repository name is required")
	}
	if r.FullName == "" {
		return fmt.Errorf("scm registered repository full name is required")
	}
	if r.CloneURL == "" || !isAbsoluteURL(r.CloneURL) {
		return fmt.Errorf("scm registered repository clone url is required")
	}
	if r.WebURL == "" || !isAbsoluteURL(r.WebURL) {
		return fmt.Errorf("scm registered repository web url is required")
	}
	if r.MetadataRefreshedAt.IsZero() {
		return fmt.Errorf("scm registered repository metadata refreshed at is required")
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return fmt.Errorf("scm registered repository timestamps are required")
	}
	return nil
}
