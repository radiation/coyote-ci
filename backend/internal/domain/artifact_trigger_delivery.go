package domain

import "time"

type ArtifactTriggerDeliveryStatus string

const (
	ArtifactTriggerDeliveryStatusPending ArtifactTriggerDeliveryStatus = "pending"
	ArtifactTriggerDeliveryStatusQueued  ArtifactTriggerDeliveryStatus = "queued"
	ArtifactTriggerDeliveryStatusFailed  ArtifactTriggerDeliveryStatus = "failed"
)

type ArtifactTriggerDelivery struct {
	ID                string
	ArtifactID        string
	ConsumerJobID     string
	ProducerBuildID   string
	ProducerProjectID string
	ProducerJobID     string
	ArtifactPath      string
	QueuedBuildID     *string
	ErrorMessage      *string
	Status            ArtifactTriggerDeliveryStatus
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
