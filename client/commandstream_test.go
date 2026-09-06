package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/seabird-chat/seabird-go/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeSource struct {
	ch       chan *pb.Event
	closed   bool
	closeErr error
}

func (f *fakeSource) Events() <-chan *pb.Event { return f.ch }
func (f *fakeSource) Close() error             { f.closed = true; return f.closeErr }

func noWait(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }

func TestCommandStreamReconnectsAfterClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opens, handled := 0, 0
	first := &fakeSource{ch: make(chan *pb.Event, 1)}
	first.ch <- &pb.Event{Inner: &pb.Event_Message{Message: &pb.MessageEvent{Text: "hi"}}}
	close(first.ch)
	second := &fakeSource{ch: make(chan *pb.Event)}

	cs := &commandStream{
		open: func() (eventSource, error) {
			opens++
			if opens == 1 {
				return first, nil
			}
			cancel()
			return second, nil
		},
		handle: func(*pb.Event) { handled++ },
		wait:   noWait,
	}

	if err := cs.run(ctx); err != nil {
		t.Fatalf("run returned %v, want nil on context cancel", err)
	}
	if opens != 2 || handled != 1 {
		t.Errorf("opens=%d handled=%d, want 2 and 1", opens, handled)
	}
	if !first.closed || !second.closed {
		t.Errorf("streams should be closed: first=%v second=%v", first.closed, second.closed)
	}
}

func TestCommandStreamRetriesTransientOpenErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opens := 0
	cs := &commandStream{
		open: func() (eventSource, error) {
			opens++
			if opens < 3 {
				return nil, errors.New("connection refused")
			}
			cancel()
			return &fakeSource{ch: make(chan *pb.Event)}, nil
		},
		handle: func(*pb.Event) {},
		wait:   noWait,
	}
	if err := cs.run(ctx); err != nil {
		t.Fatalf("run returned %v", err)
	}
	if opens != 3 {
		t.Errorf("opens = %d, want 3", opens)
	}
}

// seabird-core reports a duplicate registration either by refusing the open
// or by accepting the stream and closing it with the error.
func TestCommandStreamGivesUpOnPersistentAlreadyExists(t *testing.T) {
	refused := status.Error(codes.AlreadyExists, `command "noaa" already registered by another plugin`)
	openers := map[string]func() (eventSource, error){
		"refused on open": func() (eventSource, error) { return nil, refused },
		"closed after open": func() (eventSource, error) {
			src := &fakeSource{ch: make(chan *pb.Event), closeErr: refused}
			close(src.ch)
			return src, nil
		},
	}
	for name, open := range openers {
		opens := 0
		cs := &commandStream{
			open:             func() (eventSource, error) { opens++; return open() },
			handle:           func(*pb.Event) {},
			wait:             noWait,
			maxAlreadyExists: 3,
		}
		if err := cs.run(context.Background()); err == nil {
			t.Errorf("%s: run should give up with an error", name)
		}
		if opens != 3 {
			t.Errorf("%s: opens = %d, want 3", name, opens)
		}
	}
}

func TestCommandStreamBacksOffOnInstantCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var delays []time.Duration
	opens := 0
	cs := &commandStream{
		open: func() (eventSource, error) {
			opens++
			if opens == 4 {
				cancel()
			}
			src := &fakeSource{ch: make(chan *pb.Event)}
			close(src.ch)
			return src, nil
		},
		handle: func(*pb.Event) {},
		wait: func(ctx context.Context, d time.Duration) bool {
			delays = append(delays, d)
			return ctx.Err() == nil
		},
	}
	if err := cs.run(ctx); err != nil {
		t.Fatalf("run returned %v", err)
	}
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	if len(delays) != 3 || delays[0] != want[0] || delays[1] != want[1] || delays[2] != want[2] {
		t.Errorf("delays = %v, want %v", delays, want)
	}
}
