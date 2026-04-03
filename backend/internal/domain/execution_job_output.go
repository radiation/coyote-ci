package domain

import "time"

type ExecutionJobOutputStatus string

const (
	ExecutionJobOutputStatusDeclared  ExecutionJobOutputStatus = "declared"
	ExecutionJobOutputStatusAvailable ExecutionJobOutputStatus = "available"
	ExecutionJobOutputStatusMissing   ExecutionJobOutputStatus = "missing"
)

type ExecutionJobOutput struct {
	ID             string
	JobID          string
	BuildID        string
	Name           string
	Kind           string
	DeclaredPath   string
	DestinationURI *string
	ContentType    *string
	SizeBytes      *int64
	Digest         *string
	Status         ExecutionJobOutputStatus
	CreatedAt      time.Time
}
