package nwwsio

import (
	"encoding/xml"

	"gosrc.io/xmpp/stanza"
)

// MUCUserPresence is the XEP-0045 muc#user extension on room presences; its
// status codes explain them (110 own presence, 307 kicked, 332 shutdown).
type MUCUserPresence struct {
	stanza.PresExtension
	XMLName xml.Name    `xml:"http://jabber.org/protocol/muc#user x"`
	Item    MUCItem     `xml:"item"`
	Status  []MUCStatus `xml:"status"`
}

type MUCItem struct {
	Affiliation string `xml:"affiliation,attr"`
	Role        string `xml:"role,attr"`
}

type MUCStatus struct {
	Code int `xml:"code,attr"`
}

func (m *MUCUserPresence) StatusCodes() []int {
	codes := make([]int, len(m.Status))
	for i, s := range m.Status {
		codes[i] = s.Code
	}
	return codes
}

func (m *MUCUserPresence) HasStatus(code int) bool {
	for _, s := range m.Status {
		if s.Code == code {
			return true
		}
	}
	return false
}

func init() {
	stanza.TypeRegistry.MapExtension(stanza.PKTPresence, xml.Name{Space: "http://jabber.org/protocol/muc#user", Local: "x"}, MUCUserPresence{})
}
