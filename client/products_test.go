package client

import (
	"testing"

	nwwsio "github.com/seabird-chat/seabird-nwwsio-plugin/internal"
	"gosrc.io/xmpp/stanza"
)

func TestIsTestMessage(t *testing.T) {
	cases := []struct {
		name string
		x    nwwsio.NWWSOIMessageXExtension
		cap  *nwwsio.Alert
		want bool
	}{
		{"NTXX98 keepalive", nwwsio.NWWSOIMessageXExtension{Ttaaii: "NTXX98", Cccc: "KILM"}, nil, true},
		{"TST product under a normal header", nwwsio.NWWSOIMessageXExtension{Ttaaii: "NOUS41", AwipsID: "TSTBOX "}, nil, true},
		{"CAP with status Test", nwwsio.NWWSOIMessageXExtension{Ttaaii: "XOUS56", AwipsID: "CAPWBC"}, &nwwsio.Alert{Status: "Test"}, true},
		{"CAP with status Actual", nwwsio.NWWSOIMessageXExtension{Ttaaii: "XOUS56", AwipsID: "CAPEKA"}, &nwwsio.Alert{Status: "Actual"}, false},
		{"tornado warning", nwwsio.NWWSOIMessageXExtension{Ttaaii: "WFUS52", AwipsID: "TORJAX"}, nil, false},
	}
	for _, c := range cases {
		if got := isTestMessage(&c.x, c.cap); got != c.want {
			t.Errorf("%s: isTestMessage = %v, want %v", c.name, got, c.want)
		}
	}
}

func nwwsStanza(x nwwsio.NWWSOIMessageXExtension) stanza.Message {
	msg := stanza.NewMessage(stanza.Attrs{Type: stanza.MessageTypeGroupchat, From: "nwws@conference.nwws-oi.weather.gov/nwws-oi"})
	msg.Extensions = append(msg.Extensions, &x)
	return msg
}

func TestHandleMessageDropsTestTrafficWhenFiltering(t *testing.T) {
	keepalive := nwwsio.NWWSOIMessageXExtension{
		Cccc: "KILM", Ttaaii: "NTXX98", Issue: "2026-09-06T03:28:00Z", ID: "1.1",
		Text: "\n222\nNTXX98 KILM 060328\n\nTHIS IS A TEST MESSAGE FROM ILM\n",
	}
	product := nwwsio.NWWSOIMessageXExtension{
		Cccc: "KJAX", Ttaaii: "SRUS42", AwipsID: "RRMJAX", Issue: "2026-09-06T03:29:00Z", ID: "1.2",
		Text: "\n384\nSRUS42 KJAX 060329\nRRMJAX\n\n.A DATA\n",
	}

	for _, filtering := range []bool{true, false} {
		c := &SeabirdClient{
			subscriptions:      NewSubscriptionManager(),
			lastSequence:       make(map[string]int),
			filterTestMessages: filtering,
		}
		handleMessage(nwwsStanza(keepalive), c)
		handleMessage(nwwsStanza(product), c)

		wantKeepalive := 1
		if filtering {
			wantKeepalive = 0
		}
		gotKeepalive := len(c.subscriptions.GetRecentMessages("KILM"))
		gotProduct := len(c.subscriptions.GetRecentMessages("KJAX"))
		if gotKeepalive != wantKeepalive || gotProduct != 1 {
			t.Errorf("filtering=%v: keepalive recorded %d times (want %d), product %d (want 1)", filtering, gotKeepalive, wantKeepalive, gotProduct)
		}
	}
}
