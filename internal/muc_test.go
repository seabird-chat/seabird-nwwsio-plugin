package nwwsio

import (
	"encoding/xml"
	"reflect"
	"testing"

	"gosrc.io/xmpp/stanza"
)

func TestMUCUserPresenceParses(t *testing.T) {
	raw := `<presence xmlns="jabber:client" from="nwws@conference.nwws-oi.weather.gov/wind.060-abc" to="wind.060@nwws-oi.weather.gov/nwws-abc" type="unavailable">` +
		`<x xmlns="http://jabber.org/protocol/muc#user"><item affiliation="none" role="none"/><status code="307"/><status code="110"/></x></presence>`

	var pres stanza.Presence
	if err := xml.Unmarshal([]byte(raw), &pres); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var user MUCUserPresence
	if !pres.Get(&user) {
		t.Fatalf("muc#user extension not parsed; extensions = %#v", pres.Extensions)
	}
	if !reflect.DeepEqual(user.StatusCodes(), []int{307, 110}) {
		t.Errorf("StatusCodes = %v, want [307 110]", user.StatusCodes())
	}
	if !user.HasStatus(307) || user.HasStatus(332) {
		t.Errorf("HasStatus wrong: 307=%v 332=%v", user.HasStatus(307), user.HasStatus(332))
	}
	if user.Item.Role != "none" {
		t.Errorf("Item.Role = %q, want none", user.Item.Role)
	}
}
