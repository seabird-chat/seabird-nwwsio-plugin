package client

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// Consecutive "command already registered" refusals tolerated before
	// concluding another instance of the plugin really is running.
	defaultMaxAlreadyExists = 5
	// A stream that lives this long without events is healthy; streams that
	// close sooner back off exponentially.
	healthyStreamDuration = time.Minute
)

type eventSource interface {
	Events() <-chan *pb.Event
	Close() error
}

type seabirdEventSource struct {
	*seabird.EventStream
}

func (s seabirdEventSource) Events() <-chan *pb.Event { return s.C }

// commandStream keeps a seabird event stream open, reconnecting with backoff
// whenever it closes. seabird-core reports a duplicate registration either
// by refusing the open or by accepting the stream and closing it at once
// with an AlreadyExists status, so both paths are checked.
type commandStream struct {
	open             func() (eventSource, error)
	handle           func(*pb.Event)
	wait             func(ctx context.Context, d time.Duration) bool
	maxAlreadyExists int              // 0 means defaultMaxAlreadyExists
	now              func() time.Time // nil means time.Now
}

// run blocks until ctx ends (returning nil) or the stream can no longer be
// re-established because another instance holds the command registration.
func (cs *commandStream) run(ctx context.Context) error {
	maxAlreadyExists := cs.maxAlreadyExists
	if maxAlreadyExists == 0 {
		maxAlreadyExists = defaultMaxAlreadyExists
	}
	now := cs.now
	if now == nil {
		now = time.Now
	}

	failures, alreadyExists := 0, 0
	for ctx.Err() == nil {
		src, err := cs.open()
		if err != nil {
			failures++
			if status.Code(err) == codes.AlreadyExists {
				alreadyExists++
				if alreadyExists >= maxAlreadyExists {
					return fmt.Errorf("another instance of this plugin is already running (command registration refused %d times): %w", alreadyExists, err)
				}
			} else {
				alreadyExists = 0
			}
			delay := reconnectDelay(failures - 1)
			log.Warn().Err(err).Int("attempt", failures).Dur("retry_in", delay).Msg("Failed to open seabird event stream, retrying")
			if !cs.wait(ctx, delay) {
				return nil
			}
			continue
		}

		log.Info().Msg("Event stream established - ready to receive commands")
		opened := now()
		closeErr, events := cs.consume(ctx, src)
		if ctx.Err() != nil {
			return nil
		}

		if status.Code(closeErr) == codes.AlreadyExists {
			alreadyExists++
			if alreadyExists >= maxAlreadyExists {
				return fmt.Errorf("another instance of this plugin is already running (command registration refused %d times): %w", alreadyExists, closeErr)
			}
		} else {
			alreadyExists = 0
		}
		if events > 0 || now().Sub(opened) >= healthyStreamDuration {
			failures = 0
		} else {
			failures++
		}
		delay := reconnectDelay(max(failures-1, 0))
		log.Warn().Err(closeErr).Int("events", events).Int("attempt", failures).Dur("retry_in", delay).Msg("Seabird event stream closed, reconnecting")
		if !cs.wait(ctx, delay) {
			return nil
		}
	}
	return nil
}

// consume reads events until the stream closes or ctx ends, returning the
// close error (nil when ctx ended) and the number of events handled.
func (cs *commandStream) consume(ctx context.Context, src eventSource) (error, int) {
	count := 0
	for {
		select {
		case <-ctx.Done():
			log.Info().Int("events", count).Msg("Context cancelled - closing event stream")
			if err := src.Close(); err != nil {
				log.Debug().Err(err).Msg("Event stream close reported an error during shutdown")
			}
			return nil, count
		case event, ok := <-src.Events():
			if !ok {
				return src.Close(), count
			}
			count++
			cs.handle(event)
		}
	}
}
