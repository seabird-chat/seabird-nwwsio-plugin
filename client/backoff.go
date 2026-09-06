package client

import (
	"context"
	"time"
)

const (
	reconnectBaseDelay = 1 * time.Second
	reconnectMaxDelay  = 30 * time.Second
)

// reconnectDelay doubles from 1s per attempt (0-based) and caps at 30s.
func reconnectDelay(attempt int) time.Duration {
	delay := reconnectBaseDelay
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay >= reconnectMaxDelay {
			return reconnectMaxDelay
		}
	}
	return delay
}

// waitFor sleeps for d unless ctx ends first, reporting whether it completed.
func waitFor(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
