package client

import (
	"reflect"
	"testing"
)

func TestGeoRecipients(t *testing.T) {
	sm := NewSubscriptionManager()
	sm.SubscribeToSAME("capOnly", []string{"012031"}, nil)
	sm.SubscribeToSAME("warnings", []string{"012031"}, []string{"warning"})
	sm.SubscribeToSAME("everything", []string{"012031", "012109"}, []string{"all"})
	sm.SubscribeToZIP("zipCap", []string{"48103"}, nil) // Washtenaw, MI = 026161
	sm.SubscribeToZIP("zipAll", []string{"48103"}, []string{"all"})
	sm.SubscribeToSAME("elsewhere", []string{"048201"}, []string{"all"})

	cases := []struct {
		name     string
		codes    []string
		category string
		isCAP    bool
		want     []geoRecipient
	}{
		{"CAP alert over two counties", []string{"012031", "012109"}, "Administrative", true, []geoRecipient{
			{UserID: "capOnly", Matched: []string{"012031 (Duval, FL)"}},
			{UserID: "everything", Matched: []string{"012031 (Duval, FL)", "012109 (St. Johns, FL)"}},
		}},
		{"text warning reaches category and all filters only", []string{"012031"}, "Warning", false, []geoRecipient{
			{UserID: "everything", Matched: []string{"012031 (Duval, FL)"}},
			{UserID: "warnings", Matched: []string{"012031 (Duval, FL)"}},
		}},
		{"text forecast reaches the zip 'all' subscriber via county", []string{"026161"}, "Forecast", false, []geoRecipient{
			{UserID: "zipAll", Matched: []string{"ZIP 48103 (026161 Washtenaw, MI)"}},
		}},
		{"CAP reaches both zip subscribers", []string{"026161"}, "Administrative", true, []geoRecipient{
			{UserID: "zipAll", Matched: []string{"ZIP 48103 (026161 Washtenaw, MI)"}},
			{UserID: "zipCap", Matched: []string{"ZIP 48103 (026161 Washtenaw, MI)"}},
		}},
		{"no coverage", []string{"999999"}, "Warning", true, []geoRecipient{}},
	}
	for _, c := range cases {
		if got := geoRecipients(sm, c.codes, c.category, c.isCAP); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: recipients = %v, want %v", c.name, got, c.want)
		}
	}
}
