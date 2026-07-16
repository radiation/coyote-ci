package domain

import (
	"fmt"
	"strings"
	"time"
)

type SCMCommitStatusState string

const (
	SCMCommitStatusStatePending SCMCommitStatusState = "pending"
	SCMCommitStatusStateSuccess SCMCommitStatusState = "success"
	SCMCommitStatusStateFailure SCMCommitStatusState = "failure"
	SCMCommitStatusStateError   SCMCommitStatusState = "error"
)

type SCMStatusDeliveryStatus string

const (
	SCMStatusDeliveryStatusPending         SCMStatusDeliveryStatus = "pending"
	SCMStatusDeliveryStatusSending         SCMStatusDeliveryStatus = "sending"
	SCMStatusDeliveryStatusRetryWaiting    SCMStatusDeliveryStatus = "retry_waiting"
	SCMStatusDeliveryStatusSent            SCMStatusDeliveryStatus = "sent"
	SCMStatusDeliveryStatusFailedPermanent SCMStatusDeliveryStatus = "failed_permanent"
	SCMStatusDeliveryStatusFailedExhausted SCMStatusDeliveryStatus = "failed_exhausted"
	SCMStatusDeliveryStatusSuperseded      SCMStatusDeliveryStatus = "superseded"
)

type SCMStatusDeliveryFailureCategory string

const (
	SCMStatusDeliveryFailureCategoryRetryable SCMStatusDeliveryFailureCategory = "retryable"
	SCMStatusDeliveryFailureCategoryPermanent SCMStatusDeliveryFailureCategory = "permanent"
)

type SCMStatusDelivery struct {
	ID              string
	BuildID         string
	Provider        string
	RepositoryOwner string
	RepositoryName  string
	CommitSHA       string
	Context         string
	DesiredState    SCMCommitStatusState
	LastSentState   *SCMCommitStatusState
	Description     string
	DetailsURL      *string

	Status          SCMStatusDeliveryStatus
	Attempts        int
	MaxAttempts     int
	LastAttemptAt   *time.Time
	NextAttemptAt   *time.Time
	ClaimedAt       *time.Time
	ClaimExpiresAt  *time.Time
	ClaimedBy       *string
	FailureCategory *SCMStatusDeliveryFailureCategory
	FailureReason   *string
	LastError       *string
	SentAt          *time.Time
	SupersededAt    *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (d SCMStatusDelivery) Normalize() SCMStatusDelivery {
	d.BuildID = strings.TrimSpace(d.BuildID)
	d.Provider = strings.ToLower(strings.TrimSpace(d.Provider))
	d.RepositoryOwner = strings.TrimSpace(d.RepositoryOwner)
	d.RepositoryName = strings.TrimSpace(d.RepositoryName)
	d.CommitSHA = strings.TrimSpace(d.CommitSHA)
	d.Context = strings.TrimSpace(d.Context)
	d.Description = strings.TrimSpace(d.Description)
	d.DesiredState = SCMCommitStatusState(strings.TrimSpace(string(d.DesiredState)))
	d.Status = SCMStatusDeliveryStatus(strings.TrimSpace(string(d.Status)))
	d.DetailsURL = trimSCMOptionalString(d.DetailsURL)
	d.ClaimedBy = trimSCMOptionalString(d.ClaimedBy)
	d.FailureReason = trimSCMOptionalString(d.FailureReason)
	d.LastError = trimSCMOptionalString(d.LastError)
	d.LastAttemptAt = normalizeSCMOptionalTime(d.LastAttemptAt)
	d.NextAttemptAt = normalizeSCMOptionalTime(d.NextAttemptAt)
	d.ClaimedAt = normalizeSCMOptionalTime(d.ClaimedAt)
	d.ClaimExpiresAt = normalizeSCMOptionalTime(d.ClaimExpiresAt)
	d.SentAt = normalizeSCMOptionalTime(d.SentAt)
	d.SupersededAt = normalizeSCMOptionalTime(d.SupersededAt)
	if d.LastSentState != nil {
		trimmed := SCMCommitStatusState(strings.TrimSpace(string(*d.LastSentState)))
		if trimmed == "" {
			d.LastSentState = nil
		} else {
			d.LastSentState = &trimmed
		}
	}
	if d.FailureCategory != nil {
		trimmed := SCMStatusDeliveryFailureCategory(strings.TrimSpace(string(*d.FailureCategory)))
		if trimmed == "" {
			d.FailureCategory = nil
		} else {
			d.FailureCategory = &trimmed
		}
	}
	if d.MaxAttempts <= 0 {
		d.MaxAttempts = 1
	}
	return d
}

func (d SCMStatusDelivery) ValidateIdentity() error {
	if d.BuildID == "" {
		return fmt.Errorf("scm status delivery build id is required")
	}
	if d.Provider == "" {
		return fmt.Errorf("scm status delivery provider is required")
	}
	if d.RepositoryOwner == "" || d.RepositoryName == "" {
		return fmt.Errorf("scm status delivery repository owner and name are required")
	}
	if d.CommitSHA == "" {
		return fmt.Errorf("scm status delivery commit sha is required")
	}
	if d.Context == "" {
		return fmt.Errorf("scm status delivery context is required")
	}
	if !d.DesiredState.IsValid() {
		return fmt.Errorf("unsupported scm commit status state %q", d.DesiredState)
	}
	return nil
}

func (d SCMStatusDelivery) Validate() error {
	d = d.Normalize()
	if err := d.ValidateIdentity(); err != nil {
		return err
	}
	if d.Attempts < 0 {
		return fmt.Errorf("scm status delivery attempts cannot be negative")
	}
	if d.Attempts > d.MaxAttempts {
		return fmt.Errorf("scm status delivery attempts cannot exceed max attempts")
	}
	if !d.Status.IsValid() {
		return fmt.Errorf("unsupported scm status delivery status %q", d.Status)
	}
	if d.FailureCategory != nil && !d.FailureCategory.IsValid() {
		return fmt.Errorf("unsupported scm status delivery failure category %q", *d.FailureCategory)
	}
	if d.LastSentState != nil && !d.LastSentState.IsValid() {
		return fmt.Errorf("unsupported scm status delivery last sent state %q", *d.LastSentState)
	}
	if d.IsTerminal() && d.ClaimedBy != nil {
		return fmt.Errorf("terminal scm status delivery cannot retain an active claim owner")
	}
	if d.IsTerminal() && d.ClaimedAt != nil {
		return fmt.Errorf("terminal scm status delivery cannot retain claimed_at")
	}
	if d.IsTerminal() && d.ClaimExpiresAt != nil {
		return fmt.Errorf("terminal scm status delivery cannot retain claim_expires_at")
	}
	if d.IsTerminal() && d.NextAttemptAt != nil {
		return fmt.Errorf("terminal scm status delivery cannot retain next_attempt_at")
	}
	if d.Status == SCMStatusDeliveryStatusPending {
		if d.NextAttemptAt != nil {
			return fmt.Errorf("pending scm status delivery cannot retain next_attempt_at")
		}
		if d.ClaimedBy != nil || d.ClaimedAt != nil || d.ClaimExpiresAt != nil {
			return fmt.Errorf("pending scm status delivery cannot retain active claim metadata")
		}
	}
	if d.Status == SCMStatusDeliveryStatusRetryWaiting {
		if d.Attempts >= d.MaxAttempts {
			return fmt.Errorf("retry-waiting scm status delivery requires attempts below max attempts")
		}
		if d.NextAttemptAt == nil {
			return fmt.Errorf("retry-waiting scm status delivery requires next_attempt_at")
		}
		if d.FailureCategory == nil || *d.FailureCategory != SCMStatusDeliveryFailureCategoryRetryable {
			return fmt.Errorf("retry-waiting scm status delivery requires retryable failure category")
		}
	}
	if d.Status == SCMStatusDeliveryStatusSending {
		if d.Attempts < 1 {
			return fmt.Errorf("sending scm status delivery requires at least one attempt")
		}
		if d.ClaimedBy == nil || d.ClaimedAt == nil || d.ClaimExpiresAt == nil {
			return fmt.Errorf("sending scm status delivery requires claim owner and expiry")
		}
		if !d.ClaimExpiresAt.After(*d.ClaimedAt) {
			return fmt.Errorf("scm status delivery claim expiry must be after claim acquisition")
		}
	}
	if d.Status == SCMStatusDeliveryStatusSent {
		if d.Attempts < 1 {
			return fmt.Errorf("sent scm status delivery requires at least one attempt")
		}
		if d.SentAt == nil {
			return fmt.Errorf("sent scm status delivery requires sent_at")
		}
		if d.LastSentState == nil || *d.LastSentState != d.DesiredState {
			return fmt.Errorf("sent scm status delivery requires last_sent_state to match desired_state")
		}
	}
	if d.Status == SCMStatusDeliveryStatusFailedPermanent {
		if d.FailureCategory == nil || *d.FailureCategory != SCMStatusDeliveryFailureCategoryPermanent {
			return fmt.Errorf("permanently failed scm status delivery requires permanent failure category")
		}
	}
	if d.Status == SCMStatusDeliveryStatusFailedExhausted {
		if d.Attempts != d.MaxAttempts {
			return fmt.Errorf("exhausted scm status delivery requires attempts to equal max attempts")
		}
		if d.FailureCategory == nil || *d.FailureCategory != SCMStatusDeliveryFailureCategoryRetryable {
			return fmt.Errorf("exhausted scm status delivery requires retryable failure category")
		}
	}
	if d.Status == SCMStatusDeliveryStatusSuperseded {
		if d.SupersededAt == nil {
			return fmt.Errorf("superseded scm status delivery requires superseded_at")
		}
	}
	return nil
}

func (s SCMCommitStatusState) IsValid() bool {
	switch s {
	case SCMCommitStatusStatePending, SCMCommitStatusStateSuccess, SCMCommitStatusStateFailure, SCMCommitStatusStateError:
		return true
	default:
		return false
	}
}

func (s SCMStatusDeliveryStatus) IsValid() bool {
	switch s {
	case SCMStatusDeliveryStatusPending,
		SCMStatusDeliveryStatusSending,
		SCMStatusDeliveryStatusRetryWaiting,
		SCMStatusDeliveryStatusSent,
		SCMStatusDeliveryStatusFailedPermanent,
		SCMStatusDeliveryStatusFailedExhausted,
		SCMStatusDeliveryStatusSuperseded:
		return true
	default:
		return false
	}
}

func (c SCMStatusDeliveryFailureCategory) IsValid() bool {
	switch c {
	case SCMStatusDeliveryFailureCategoryRetryable, SCMStatusDeliveryFailureCategoryPermanent:
		return true
	default:
		return false
	}
}

func (d SCMStatusDelivery) IsTerminal() bool {
	switch d.Status {
	case SCMStatusDeliveryStatusSent,
		SCMStatusDeliveryStatusFailedPermanent,
		SCMStatusDeliveryStatusFailedExhausted,
		SCMStatusDeliveryStatusSuperseded:
		return true
	default:
		return false
	}
}

func (d SCMStatusDelivery) CanAttempt(now time.Time) bool {
	if d.IsTerminal() || d.Attempts >= d.MaxAttempts {
		return false
	}
	switch d.Status {
	case SCMStatusDeliveryStatusPending:
		return true
	case SCMStatusDeliveryStatusRetryWaiting:
		return d.NextAttemptAt != nil && !now.UTC().Before(d.NextAttemptAt.UTC())
	case SCMStatusDeliveryStatusSending:
		return d.ClaimExpiresAt != nil && !now.UTC().Before(d.ClaimExpiresAt.UTC())
	default:
		return false
	}
}

func trimSCMOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeSCMOptionalTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}
