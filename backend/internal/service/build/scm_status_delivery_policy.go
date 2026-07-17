package build

import (
	"fmt"
	"strings"
	"time"
)

const defaultSCMStatusDeliveryMaxAttempts = 3
const defaultSCMStatusDeliveryClaimDuration = 2 * time.Minute
const defaultSCMStatusRetryInitialDelay = 30 * time.Second
const defaultSCMStatusRetryMaxDelay = 10 * time.Minute

type scmStatusRetryPolicy struct {
	maxAttempts  int
	initialDelay time.Duration
	maxDelay     time.Duration
}

func defaultSCMStatusRetryPolicy() scmStatusRetryPolicy {
	return scmStatusRetryPolicy{
		maxAttempts:  defaultSCMStatusDeliveryMaxAttempts,
		initialDelay: defaultSCMStatusRetryInitialDelay,
		maxDelay:     defaultSCMStatusRetryMaxDelay,
	}
}

func (p scmStatusRetryPolicy) delayForAttempt(attempt int) time.Duration {
	if attempt <= 1 {
		return p.initialDelay
	}
	delay := p.initialDelay
	for idx := 1; idx < attempt; idx++ {
		delay *= 2
		if delay >= p.maxDelay {
			return p.maxDelay
		}
	}
	if delay > p.maxDelay {
		return p.maxDelay
	}
	return delay
}

func scmStatusClaimDuration(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return defaultSCMStatusDeliveryClaimDuration
}

func validateSCMStatusClaimDuration(value time.Duration) error {
	if value <= 0 {
		return fmt.Errorf("scm status claim duration must be positive")
	}
	return nil
}

func scmStatusClaimOwner(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return trimmed
	}
	return "inline-scm-status-reporter"
}
