package repository

import (
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func CompareSCMStatusDeliveryOwners(existing domain.SCMStatusDelivery, incoming domain.SCMStatusDelivery) int {
	existing = existing.Normalize()
	incoming = incoming.Normalize()
	if strings.TrimSpace(existing.BuildID) == strings.TrimSpace(incoming.BuildID) {
		return 0
	}
	if existing.BuildAttempt != incoming.BuildAttempt {
		if existing.BuildAttempt > incoming.BuildAttempt {
			return 1
		}
		return -1
	}
	if existing.BuildCreatedAt.After(incoming.BuildCreatedAt) {
		return 1
	}
	if existing.BuildCreatedAt.Before(incoming.BuildCreatedAt) {
		return -1
	}
	if strings.Compare(strings.TrimSpace(existing.BuildID), strings.TrimSpace(incoming.BuildID)) > 0 {
		return 1
	}
	return -1
}

func SCMStatusDeliveryIncomingStateObsolete(existing domain.SCMStatusDelivery, incoming domain.SCMStatusDelivery) bool {
	if existing.DesiredState.IsTerminal() && !incoming.DesiredState.IsTerminal() {
		return true
	}
	if existing.LastSentState != nil && existing.LastSentState.IsTerminal() && !incoming.DesiredState.IsTerminal() {
		return true
	}
	return false
}

func SCMStatusDeliveryShouldReplaceCurrentState(existing domain.SCMStatusDelivery, incoming domain.SCMStatusDelivery) bool {
	return existing.DesiredState != incoming.DesiredState
}

func SCMStatusDeliveryReassertAfterReplacement(existing domain.SCMStatusDelivery, now time.Time) *time.Time {
	existing = existing.Normalize()
	if existing.Status != domain.SCMStatusDeliveryStatusSending {
		return nil
	}
	if existing.ClaimExpiresAt == nil || !existing.ClaimExpiresAt.After(now.UTC()) {
		return nil
	}
	reassertAt := existing.ClaimExpiresAt.UTC()
	return &reassertAt
}
