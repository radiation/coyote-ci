package domain

import "time"

type ManagedImage struct {
	ID          string
	ProjectID   string
	Name        string
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ManagedImageVersion struct {
	ID                    string
	ManagedImageID        string
	VersionLabel          string
	ImageRef              string
	ImageDigest           string
	DependencyFingerprint *string
	SourceRepositoryURL   *string
	CreatedAt             time.Time
}
