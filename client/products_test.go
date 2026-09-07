package client

import (
	"reflect"
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
		{"KNCF communications test with empty AWIPS ID", nwwsio.NWWSOIMessageXExtension{Ttaaii: "WOUS99", Cccc: "KNCF"}, nil, true},
	}
	for _, c := range cases {
		if got := isTestMessage(&c.x, c.cap); got != c.want {
			t.Errorf("%s: isTestMessage = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSequenceTrackerToleratesReorderingAndDuplicates(t *testing.T) {
	tr := newSequenceTracker(1)
	for _, seq := range []int{2, 4, 3, 5, 5, 7, 8, 6} {
		if lost := tr.observe(seq); len(lost) != 0 {
			t.Errorf("observe(%d) reported lost %v, want none", seq, lost)
		}
	}
}

func TestSequenceTrackerFollowsANumberingRestart(t *testing.T) {
	tr := newSequenceTracker(10480)
	for _, seq := range []int{10481, 10482, 10483, 1, 2, 3, 5} {
		if lost := tr.observe(seq); len(lost) != 0 {
			t.Errorf("observe(%d) reported lost %v, want none", seq, lost)
		}
	}
	var lost []int
	for seq := 6; seq <= 4+sequencePatience+1; seq++ {
		lost = append(lost, tr.observe(seq)...)
	}
	if want := []int{4}; !reflect.DeepEqual(lost, want) {
		t.Errorf("lost after restart = %v, want %v", lost, want)
	}
}

func TestSequenceTrackerReportsLostNumberOncePastPatience(t *testing.T) {
	tr := newSequenceTracker(1)
	tr.observe(2)
	var lost []int
	for seq := 4; seq <= 3+sequencePatience+1; seq++ {
		lost = append(lost, tr.observe(seq)...)
	}
	if want := []int{3}; !reflect.DeepEqual(lost, want) {
		t.Errorf("lost = %v, want %v", lost, want)
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
			sequences:          make(map[string]*sequenceTracker),
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
