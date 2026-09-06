package client

import (
	"strings"
	"testing"
	"time"

	nwwsio "github.com/seabird-chat/seabird-nwwsio-plugin/internal"
	"gosrc.io/xmpp/stanza"
)

var testMUC = &stanza.Jid{Node: "nwws", Domain: "conference.nwws-oi.weather.gov", Resource: "wind.060-abc"}

func mucUser(codes ...int) *nwwsio.MUCUserPresence {
	ext := &nwwsio.MUCUserPresence{}
	for _, c := range codes {
		ext.Status = append(ext.Status, nwwsio.MUCStatus{Code: c})
	}
	return ext
}

func TestClassifyPresence(t *testing.T) {
	cases := []struct {
		name   string
		pres   stanza.Presence
		want   presenceAction
		reason string
	}{
		{"error from the room", stanza.Presence{Attrs: stanza.Attrs{From: testMUC.Full(), Type: stanza.PresenceTypeError}, Error: stanza.Err{Type: "cancel", Reason: "conflict"}}, presenceRejoin, "conflict"},
		{"kicked", stanza.Presence{Attrs: stanza.Attrs{From: testMUC.Full(), Type: stanza.PresenceTypeUnavailable}, Extensions: []stanza.PresExtension{mucUser(307, 110)}}, presenceRejoin, "307"},
		{"service shutdown", stanza.Presence{Attrs: stanza.Attrs{From: testMUC.Full(), Type: stanza.PresenceTypeUnavailable}, Extensions: []stanza.PresExtension{mucUser(332)}}, presenceRejoin, "332"},
		{"own leave without codes", stanza.Presence{Attrs: stanza.Attrs{From: testMUC.Full(), Type: stanza.PresenceTypeUnavailable}}, presenceRejoin, "left"},
		{"own join confirmation", stanza.Presence{Attrs: stanza.Attrs{From: testMUC.Full()}, Extensions: []stanza.PresExtension{mucUser(110)}}, presenceJoined, ""},
		{"another occupant leaves", stanza.Presence{Attrs: stanza.Attrs{From: testMUC.Bare() + "/someone", Type: stanza.PresenceTypeUnavailable}}, presenceIgnore, ""},
		{"another occupant present", stanza.Presence{Attrs: stanza.Attrs{From: testMUC.Bare() + "/someone"}}, presenceIgnore, ""},
		{"unrelated jid", stanza.Presence{Attrs: stanza.Attrs{From: "wind.060@nwws-oi.weather.gov/nwws-abc", Type: stanza.PresenceTypeUnavailable}}, presenceIgnore, ""},
	}
	for _, c := range cases {
		got, reason := classifyPresence(c.pres, testMUC)
		if got != c.want {
			t.Errorf("%s: action = %v, want %v", c.name, got, c.want)
		}
		if c.reason != "" && !strings.Contains(reason, c.reason) {
			t.Errorf("%s: reason %q should mention %q", c.name, reason, c.reason)
		}
	}
}

func TestWatchdogDecision(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	silence, grace := 10*time.Minute, 2*time.Minute
	var never time.Time

	cases := []struct {
		name        string
		lastMessage time.Time
		lastRejoin  time.Time
		want        watchdogAction
	}{
		{"traffic flowing", now.Add(-1 * time.Minute), never, watchdogNone},
		{"silent, no rejoin yet", now.Add(-11 * time.Minute), never, watchdogRejoin},
		{"silent, rejoin predates the silence", now.Add(-11 * time.Minute), now.Add(-30 * time.Minute), watchdogRejoin},
		{"silent, rejoin just attempted", now.Add(-11 * time.Minute), now.Add(-1 * time.Minute), watchdogNone},
		{"silent, rejoin did not help", now.Add(-13 * time.Minute), now.Add(-3 * time.Minute), watchdogDisconnect},
	}
	for _, c := range cases {
		if got := watchdogDecision(now, c.lastMessage, c.lastRejoin, silence, grace); got != c.want {
			t.Errorf("%s: decision = %v, want %v", c.name, got, c.want)
		}
	}
}
