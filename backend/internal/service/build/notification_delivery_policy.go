package build

import "time"

const defaultNotificationDeliveryMaxAttempts = 3
const defaultNotificationDeliveryClaimDuration = 2 * time.Minute
const defaultNotificationRetryInitialDelay = 30 * time.Second
const defaultNotificationRetryMaxDelay = 10 * time.Minute

type notificationRetryPolicy struct {
	maxAttempts  int
	initialDelay time.Duration
	maxDelay     time.Duration
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
