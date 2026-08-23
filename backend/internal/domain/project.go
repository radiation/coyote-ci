package domain

import "time"

const DefaultProjectSlug = "default"

type Project struct {
	ID          string
	Name        string
	Slug        string
	Description *string
	IsPublic    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
