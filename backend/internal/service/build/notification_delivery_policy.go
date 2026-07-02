package build

import (
	"fmt"
	"time"

	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
)

const defaultNotificationDeliveryMaxAttempts = 3
const defaultNotificationDeliveryClaimDuration = 2 * time.Minute
const defaultNotificationRetryInitialDelay = 30 * time.Second
const defaultNotificationRetryMaxDelay = 10 * time.Minute
const notificationClaimSafetyMargin = 30 * time.Second

type notificationRetryPolicy struct {
	maxAttempts  int
	initialDelay time.Duration
	maxDelay     time.Duration
}

func minimumNotificationClaimDuration() time.Duration {
	// This bound tracks Coyote's production-configured provider adapters.
	// Test doubles and custom HTTPDoer implementations are expected to honor
	// the same operational timeout contract without expanding this interface.
	maxProviderTimeout := platformemail.DefaultSMTPTimeout
	if platformslack.DefaultAPITimeout > maxProviderTimeout {
		maxProviderTimeout = platformslack.DefaultAPITimeout
	}
	if defaultSlackWebhookTimeout > maxProviderTimeout {
		maxProviderTimeout = defaultSlackWebhookTimeout
	}
	return maxProviderTimeout + notificationClaimSafetyMargin
}

func validateNotificationClaimDuration(value time.Duration) error {
	minimum := minimumNotificationClaimDuration()
	if value < minimum {
		return fmt.Errorf("notification claim duration %s is too short: must be at least %s to exceed provider timeouts by the %s safety margin", value, minimum, notificationClaimSafetyMargin)
	}
	return nil
}

func defaultNotificationRetryPolicy() notificationRetryPolicy {
	return notificationRetryPolicy{
		maxAttempts:  defaultNotificationDeliveryMaxAttempts,
		initialDelay: defaultNotificationRetryInitialDelay,
		maxDelay:     defaultNotificationRetryMaxDelay,
	}
}

func (p notificationRetryPolicy) delayForAttempt(attempt int) time.Duration {
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
