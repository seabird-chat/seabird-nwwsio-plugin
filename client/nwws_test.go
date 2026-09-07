package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"gosrc.io/xmpp"
	"gosrc.io/xmpp/stanza"
)

type fakeStreamClient struct {
	connectErr     error
	disconnectWait <-chan struct{} // Disconnect blocks until this closes
}

func (f *fakeStreamClient) Connect() error           { return f.connectErr }
func (f *fakeStreamClient) Resume() error            { return f.connectErr }
func (f *fakeStreamClient) Send(stanza.Packet) error { return nil }
func (f *fakeStreamClient) SendIQ(context.Context, *stanza.IQ) (chan stanza.IQ, error) {
	return nil, nil
}
func (f *fakeStreamClient) SendRaw(string) error { return nil }
func (f *fakeStreamClient) Disconnect() error {
	if f.disconnectWait != nil {
		<-f.disconnectWait
	}
	return nil
}
func (f *fakeStreamClient) SetHandler(xmpp.EventHandler) {}

func fakeSession(connectErr error) *nwwsSession {
	return fakeSessionFor(&fakeStreamClient{connectErr: connectErr})
}

func fakeSessionFor(fake *fakeStreamClient) *nwwsSession {
	return newSession(xmpp.NewStreamManager(fake, nil), fake, &stanza.Jid{Node: "nwws", Domain: "conference.nwws-oi.weather.gov", Resource: "test"})
}

func TestNWWSSessionsDoNotWaitForASlowTeardown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	release := make(chan struct{})
	built := make(chan *nwwsSession, 2)
	builds := 0
	n := &nwwsSessions{
		build: func() (*nwwsSession, error) {
			builds++
			var s *nwwsSession
			if builds == 1 {
				s = fakeSessionFor(&fakeStreamClient{disconnectWait: release})
			} else {
				s = fakeSession(nil)
			}
			built <- s
			return s, nil
		},
		wait: noWait,
	}
	go n.run(ctx)

	first := <-built
	waitUntilRunning(t, first)
	n.stop()
	select {
	case <-built:
	case <-time.After(2 * time.Second):
		t.Fatal("rebuild waited for the old session's teardown")
	}
	close(release)
	cancel()
	n.stop()
}

func waitUntilRunning(t *testing.T, s *nwwsSession) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !s.running.Load() {
		if time.Now().After(deadline) {
			t.Fatal("session never started running")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestNWWSSessionsRebuildAfterTheCurrentOneIsStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	built := make(chan *nwwsSession, 2)
	n := &nwwsSessions{
		build: func() (*nwwsSession, error) {
			s := fakeSession(nil)
			built <- s
			return s, nil
		},
		wait: noWait,
	}
	done := make(chan error, 1)
	go func() { done <- n.run(ctx) }()

	first := <-built
	waitUntilRunning(t, first)
	n.stop() // the room watchdog gave up on this session

	second := <-built
	waitUntilRunning(t, second)
	if first == second {
		t.Fatal("expected a fresh session after stop")
	}
	cancel()
	n.stop()
	if err := <-done; err != nil {
		t.Errorf("run returned %v, want nil on context cancel", err)
	}
}

func TestNWWSSessionsGiveUpAfterRepeatedFailures(t *testing.T) {
	cases := []struct {
		name  string
		build func() (*nwwsSession, error)
	}{
		{"site unreachable", func() (*nwwsSession, error) { return nil, errors.New("no NWWS-IO site reachable") }},
		{"connect fails", func() (*nwwsSession, error) { return fakeSession(errors.New("auth failed")), nil }},
	}
	for _, c := range cases {
		builds := 0
		n := &nwwsSessions{
			build: func() (*nwwsSession, error) { builds++; return c.build() },
			wait:  noWait,
		}
		err := n.run(context.Background())
		if err == nil || builds != maxNWWSFailures {
			t.Errorf("%s: err=%v builds=%d, want an error after %d attempts", c.name, err, builds, maxNWWSFailures)
		}
	}
}
