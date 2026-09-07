package client

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"gosrc.io/xmpp"
	"gosrc.io/xmpp/stanza"
)

// NWWS-IO servers, see https://www.weather.gov/nwws/#access
const (
	NWWSCollegePark   = "nwws-oi-cprk.weather.gov"
	NWWSBoulder       = "nwws-oi-bldr.weather.gov"
	NWWSServerPort    = "5222"
	NWWSDomain        = "nwws-oi.weather.gov"
	NWWSResource      = "nwws"
	ConnectionTimeout = 3 * time.Second
	maxNWWSFailures   = 5
)

type nwwsSession struct {
	manager *xmpp.StreamManager
	client  xmpp.StreamClient
	mucJID  *stanza.Jid
	running atomic.Bool
	ended   chan struct{} // closed by stop
}

func newSession(manager *xmpp.StreamManager, client xmpp.StreamClient, mucJID *stanza.Jid) *nwwsSession {
	return &nwwsSession{manager: manager, client: client, mucJID: mucJID, ended: make(chan struct{})}
}

func (s *nwwsSession) run() error {
	s.running.Store(true)
	err := s.manager.Run()
	s.running.Store(false)
	return err
}

// Stop on a manager whose Run already returned panics on its WaitGroup, and
// closing a dead connection blocks for minutes.
func (s *nwwsSession) stop() {
	if s.running.CompareAndSwap(true, false) {
		close(s.ended)
		go s.manager.Stop()
	}
}

// The library's in-place resume can leave a session that looks connected but
// receives nothing, so sessions are only ever rebuilt.
type nwwsSessions struct {
	build func() (*nwwsSession, error)
	wait  func(ctx context.Context, d time.Duration) bool

	mu      sync.Mutex
	current *nwwsSession
}

func (n *nwwsSessions) currentSession() *nwwsSession {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.current
}

func (n *nwwsSessions) setCurrent(s *nwwsSession) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.current = s
}

func (n *nwwsSessions) prepare() error {
	s, err := n.build()
	if err != nil {
		return err
	}
	n.setCurrent(s)
	return nil
}

func (n *nwwsSessions) stop() {
	if s := n.currentSession(); s != nil {
		s.stop()
	}
}

func (n *nwwsSessions) run(ctx context.Context) error {
	failures := 0
	for ctx.Err() == nil {
		session := n.currentSession()
		if session == nil {
			s, err := n.build()
			if err != nil {
				failures++
				if failures >= maxNWWSFailures {
					return fmt.Errorf("could not establish an NWWS-IO session (%d consecutive failures): %w", failures, err)
				}
				delay := reconnectDelay(failures - 1)
				log.Warn().Err(err).Int("attempt", failures).Dur("retry_in", delay).Msg("Failed to build NWWS-IO session, retrying")
				if !n.wait(ctx, delay) {
					return nil
				}
				continue
			}
			session = s
			n.setCurrent(s)
		}

		done := make(chan error, 1)
		go func() { done <- session.run() }()
		var err error
		select {
		case err = <-done:
		case <-session.ended:
			go func() {
				log.Info().Err(<-done).Str("jid", session.mucJID.Full()).Msg("Previous NWWS-IO session finished closing")
			}()
		}
		n.setCurrent(nil)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			failures++
			if failures >= maxNWWSFailures {
				return fmt.Errorf("could not establish an NWWS-IO session (%d consecutive failures): %w", failures, err)
			}
		} else {
			failures = 0
		}
		delay := reconnectDelay(max(failures-1, 0))
		log.Warn().Err(err).Int("attempt", failures).Dur("retry_in", delay).Msg("NWWS-IO session ended, rebuilding")
		if !n.wait(ctx, delay) {
			return nil
		}
	}
	return nil
}

func newNWWSSession(username, password string, router *xmpp.Router, monitor *mucMonitor) (*nwwsSession, error) {
	instanceID := generateInstanceID()
	config, err := findReachableNWWSSite(username, password, instanceID)
	if err != nil {
		return nil, err
	}
	xmppClient, err := xmpp.NewClient(config, router, xmppErrorHandler)
	if err != nil {
		return nil, err
	}
	room := &stanza.Jid{
		Node:     "nwws",
		Domain:   "conference.nwws-oi.weather.gov",
		Resource: fmt.Sprintf("%s-%s", username, instanceID),
	}
	manager := xmpp.NewStreamManager(xmppClient, func(s xmpp.Sender) { onNWWSConnected(s, monitor, room) })
	return newSession(manager, xmppClient, room), nil
}

func newNWWSRouter(client *SeabirdClient, monitor *mucMonitor) *xmpp.Router {
	router := xmpp.NewRouter()
	router.HandleFunc("message", func(_ xmpp.Sender, p stanza.Packet) { handleMessage(p, client) })
	router.HandleFunc("presence", func(_ xmpp.Sender, p stanza.Packet) { handlePresence(p, monitor) })
	router.NewRoute().IQNamespaces("jabber:iq:version").HandlerFunc(handleVersion)
	return router
}

// findReachableNWWSSite logs in to each site with a throwaway connection and
// returns the config for the first one that works.
func findReachableNWWSSite(username, password, instanceID string) (*xmpp.Config, error) {
	config := xmpp.Config{
		Jid:            fmt.Sprintf("%s@%s/%s-%s", username, NWWSDomain, NWWSResource, instanceID),
		Credential:     xmpp.Password(password),
		ConnectTimeout: int(ConnectionTimeout.Seconds()),
	}

	for _, host := range []string{NWWSCollegePark, NWWSBoulder} {
		config.TransportConfiguration = xmpp.TransportConfiguration{
			Address: host + ":" + NWWSServerPort,
			Domain:  NWWSDomain,
		}
		probe, err := xmpp.NewClient(&config, xmpp.NewRouter(), probeErrorHandler)
		if err != nil {
			return nil, err
		}

		log.Info().Str("site", config.Address).Msg("Testing connection to NWWS-IO site")
		if err := probe.Connect(); err != nil {
			log.Warn().Err(err).Str("site", host).Msg("NWWS-IO site unreachable, trying next")
			_ = probe.Disconnect()
			continue
		}
		if err := probe.Disconnect(); err != nil {
			return nil, err
		}
		return &config, nil
	}
	return nil, fmt.Errorf("failed to connect to any NWWS-IO site")
}

func onNWWSConnected(s xmpp.Sender, monitor *mucMonitor, room *stanza.Jid) {
	log.Info().Str("jid", room.Full()).Msg("NWWS-IO connection established")
	monitor.connected(s, room)
	if err := joinMUC(s, room); err != nil {
		log.Error().Err(err).Msg("Failed to send Multi-user Chat join")
		monitor.requestRejoin("initial join send failed")
		return
	}
	log.Info().Str("jid", room.Full()).Msg("Sent Multi-user Chat join - ready to receive messages")
}

func joinMUC(s xmpp.Sender, room *stanza.Jid) error {
	log.Info().Str("jid", room.Full()).Msg("Joining Multi-user Chat")
	return s.Send(stanza.Presence{
		Attrs: stanza.Attrs{To: room.Full()},
		Extensions: []stanza.PresExtension{
			stanza.MucPresence{History: stanza.History{MaxStanzas: stanza.NewNullableInt(0)}},
		},
	})
}

// The probe's receive loop reports its own teardown as an error once it is
// disconnected, so its errors are only worth a debug line.
func probeErrorHandler(err error) {
	log.Debug().Err(err).Msg("XMPP error on site probe connection")
}

func xmppErrorHandler(err error) {
	log.Error().Err(err).Msg("XMPP error")
}

func handleVersion(s xmpp.Sender, p stanza.Packet) {
	iq, ok := p.(*stanza.IQ)
	if !ok {
		return
	}
	resp, err := stanza.NewIQ(stanza.Attrs{Type: "result", From: iq.To, To: iq.From, Id: iq.Id, Lang: "en"})
	if err != nil {
		return
	}
	resp.Version().SetInfo("seabird-nwwsio-plugin", Version, fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))
	_ = s.Send(resp)
}
