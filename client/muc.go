package client

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	nwwsio "github.com/seabird-chat/seabird-nwwsio-plugin/internal"
	"gosrc.io/xmpp"
	"gosrc.io/xmpp/stanza"
)

const (
	// NWWS-IO is never quiet this long; even at night the hourly test-message
	// burst arrives. Longer silence means we are no longer in the room.
	mucSilenceTimeout = 10 * time.Minute
	// How long a rejoin gets to restore traffic before the XMPP session is
	// torn down so the stream manager reconnects from scratch.
	mucRejoinGrace  = 2 * time.Minute
	mucWatchdogTick = 30 * time.Second
)

// handlePresence watches room presences for signs that we are no longer in
// it. The router hands presence stanzas by value.
func handlePresence(p stanza.Packet, monitor *mucMonitor) {
	presence, ok := p.(stanza.Presence)
	if !ok {
		return
	}
	log.Debug().Str("from", presence.From).Str("type", string(presence.Type)).Msg("Received presence stanza")

	switch action, reason := classifyPresence(presence, monitor.mucJID); action {
	case presenceJoined:
		monitor.noteJoined()
	case presenceRejoin:
		monitor.requestRejoin(reason)
	}
}

type presenceAction int

const (
	presenceIgnore presenceAction = iota
	presenceJoined
	presenceRejoin
)

// XEP-0045 status codes that explain an involuntary departure
var mucLeaveReasons = map[int]string{
	301: "banned from room",
	307: "kicked from room",
	321: "removed after affiliation change",
	322: "removed because room became members-only",
	332: "room service shutting down",
	333: "removed after connection error",
}

func classifyPresence(pres stanza.Presence, mucJID *stanza.Jid) (presenceAction, string) {
	if !strings.HasPrefix(pres.From, mucJID.Bare()) {
		return presenceIgnore, ""
	}
	own := pres.From == mucJID.Full()

	switch pres.Type {
	case stanza.PresenceTypeError:
		reason := pres.Error.Reason
		if reason == "" {
			reason = string(pres.Error.Type)
		}
		if reason == "" {
			reason = "unspecified error"
		}
		return presenceRejoin, "error presence from room: " + reason

	case stanza.PresenceTypeUnavailable:
		if !own {
			return presenceIgnore, ""
		}
		codes := mucStatusCodes(pres)
		for _, code := range codes {
			if reason, known := mucLeaveReasons[code]; known {
				return presenceRejoin, fmt.Sprintf("%s (%d)", reason, code)
			}
		}
		if len(codes) > 0 {
			return presenceRejoin, fmt.Sprintf("left room (status %v)", codes)
		}
		return presenceRejoin, "left room"

	default:
		if own {
			return presenceJoined, ""
		}
		return presenceIgnore, ""
	}
}

func mucStatusCodes(pres stanza.Presence) []int {
	for _, ext := range pres.Extensions {
		if user, ok := ext.(*nwwsio.MUCUserPresence); ok {
			return user.StatusCodes()
		}
	}
	return nil
}

type watchdogAction int

const (
	watchdogNone watchdogAction = iota
	watchdogRejoin
	watchdogDisconnect
)

func watchdogDecision(now, lastMessage, lastRejoin time.Time, silence, grace time.Duration) watchdogAction {
	if now.Sub(lastMessage) < silence {
		return watchdogNone
	}
	if !lastRejoin.After(lastMessage) {
		return watchdogRejoin
	}
	if now.Sub(lastRejoin) >= grace {
		return watchdogDisconnect
	}
	return watchdogNone
}

// mucMonitor keeps us in the room: it rejoins with backoff when the room says
// we left, and its watchdog treats prolonged silence as lost membership.
type mucMonitor struct {
	mucJID     *stanza.Jid
	disconnect func()

	mu             sync.Mutex
	sender         xmpp.Sender
	lastMessage    time.Time
	lastRejoin     time.Time
	rejoinAttempts int
	rejoinPending  bool
	stopped        bool
}

func newMUCMonitor(mucJID *stanza.Jid) *mucMonitor {
	return &mucMonitor{mucJID: mucJID}
}

func (m *mucMonitor) connected(s xmpp.Sender) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sender = s
	m.lastMessage = time.Now()
	m.lastRejoin = time.Time{}
	m.rejoinAttempts = 0
}

func (m *mucMonitor) noteMessage() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastMessage = time.Now()
	m.rejoinAttempts = 0
}

func (m *mucMonitor) noteJoined() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastMessage = time.Now()
	m.rejoinAttempts = 0
	log.Info().Str("muc_jid", m.mucJID.Full()).Msg("Room confirmed our membership")
}

func (m *mucMonitor) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
}

// requestRejoin schedules one rejoin after a backoff delay; requests while
// one is pending are ignored.
func (m *mucMonitor) requestRejoin(reason string) {
	m.mu.Lock()
	if m.stopped || m.rejoinPending || m.sender == nil {
		m.mu.Unlock()
		return
	}
	m.rejoinPending = true
	attempt := m.rejoinAttempts
	m.rejoinAttempts++
	sender := m.sender
	m.mu.Unlock()

	delay := reconnectDelay(attempt)
	log.Warn().Str("reason", reason).Int("attempt", attempt+1).Dur("delay", delay).Msg("Lost MUC membership, scheduling rejoin")

	go func() {
		time.Sleep(delay)
		m.mu.Lock()
		m.rejoinPending = false
		m.lastRejoin = time.Now()
		stopped := m.stopped
		m.mu.Unlock()
		if stopped {
			return
		}
		if err := joinMUC(sender, m.mucJID); err != nil {
			log.Error().Err(err).Msg("Failed to send MUC rejoin")
			m.requestRejoin("rejoin send failed")
			return
		}
		log.Info().Str("muc_jid", m.mucJID.Full()).Msg("Sent MUC rejoin")
	}()
}

func (m *mucMonitor) watchdog(ctx context.Context) {
	ticker := time.NewTicker(mucWatchdogTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			m.mu.Lock()
			decision := watchdogDecision(now, m.lastMessage, m.lastRejoin, mucSilenceTimeout, mucRejoinGrace)
			silentFor := now.Sub(m.lastMessage).Round(time.Second)
			stopped := m.stopped
			m.mu.Unlock()
			if stopped {
				return
			}
			switch decision {
			case watchdogRejoin:
				m.requestRejoin(fmt.Sprintf("no room traffic for %s", silentFor))
			case watchdogDisconnect:
				log.Error().Dur("silent_for", silentFor).Msg("Room still silent after rejoin, forcing NWWS-IO reconnect")
				m.mu.Lock()
				m.lastMessage = now
				m.mu.Unlock()
				if m.disconnect != nil {
					m.disconnect()
				}
			}
		}
	}
}
