package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGeoSubscriptionLifecycle(t *testing.T) {
	kinds := []struct {
		name        string
		subscribe   func(sm *SubscriptionManager, user string, codes, filters []string)
		get         func(sm *SubscriptionManager, code string) []Subscription
		mine        func(sm *SubscriptionManager, user string) []UserSubscription
		unsubscribe func(sm *SubscriptionManager, user string, codes []string) []string
		unsubAll    func(sm *SubscriptionManager, user string) int
	}{
		{"same", (*SubscriptionManager).SubscribeToSAME, (*SubscriptionManager).GetSAMESubscriptions, (*SubscriptionManager).GetUserSAMESubscriptions, (*SubscriptionManager).UnsubscribeFromSAME, (*SubscriptionManager).UnsubscribeFromAllSAME},
		{"zip", (*SubscriptionManager).SubscribeToZIP, (*SubscriptionManager).GetZIPSubscriptions, (*SubscriptionManager).GetUserZIPSubscriptions, (*SubscriptionManager).UnsubscribeFromZIP, (*SubscriptionManager).UnsubscribeFromAllZIP},
	}
	for _, k := range kinds {
		sm := NewSubscriptionManager()
		k.subscribe(sm, "u1", []string{"B", "A"}, nil)
		k.subscribe(sm, "u2", []string{"A"}, []string{"Warning"})

		if got := k.get(sm, "A"); !reflect.DeepEqual(got, []Subscription{{UserID: "u1", Filters: []string{"cap"}}, {UserID: "u2", Filters: []string{"warning"}}}) {
			t.Errorf("%s: default cap and lowercased filters expected, got %v", k.name, got)
		}
		if got := k.mine(sm, "u1"); !reflect.DeepEqual(got, []UserSubscription{{Code: "A", Filters: []string{"cap"}}, {Code: "B", Filters: []string{"cap"}}}) {
			t.Errorf("%s: user list should be sorted by code, got %v", k.name, got)
		}

		k.subscribe(sm, "u1", []string{"A"}, []string{"all"})
		if got := k.get(sm, "A"); len(got) != 2 || got[1].UserID != "u1" || got[1].Filters[0] != "all" {
			t.Errorf("%s: resubscribe should replace filters without duplicating, got %v", k.name, got)
		}

		if removed := k.unsubscribe(sm, "u1", []string{"A", "nope"}); !reflect.DeepEqual(removed, []string{"A"}) {
			t.Errorf("%s: removed = %v, want [A]", k.name, removed)
		}
		if got := k.get(sm, "A"); len(got) != 1 || got[0].UserID != "u2" {
			t.Errorf("%s: u2 should remain on A, got %v", k.name, got)
		}
		if n := k.unsubAll(sm, "u1"); n != 1 {
			t.Errorf("%s: unsubscribe all = %d, want 1 (B)", k.name, n)
		}
		if got := k.get(sm, "B"); len(got) != 0 {
			t.Errorf("%s: empty code should be dropped, got %v", k.name, got)
		}
	}
}

func TestUnsubscribeFromAllCoversEveryKind(t *testing.T) {
	sm := NewSubscriptionManager()
	sm.SubscribeToStation("u1", "kjax", nil)
	sm.SubscribeToSAME("u1", []string{"012031"}, nil)
	sm.SubscribeToZIP("u1", []string{"48103"}, nil)
	if n := sm.UnsubscribeFromAll("u1"); n != 3 {
		t.Errorf("UnsubscribeFromAll = %d, want 3", n)
	}
	if len(sm.GetUserStations("u1"))+len(sm.GetUserSAMESubscriptions("u1"))+len(sm.GetUserZIPSubscriptions("u1")) != 0 {
		t.Error("subscriptions remain after UnsubscribeFromAll")
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subs.json")
	sm := NewSubscriptionManager()
	sm.filePath = path
	sm.SubscribeToStation("u1", "KJAX", []string{"cap"})
	sm.SubscribeToSAME("u1", []string{"012031"}, []string{"all"})
	sm.SubscribeToZIP("u1", []string{"48103"}, []string{"warning"})
	if err := sm.Save(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "stations", "same", "zip"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("saved file missing %q: %s", key, data)
		}
	}

	sm2 := NewSubscriptionManager()
	sm2.filePath = path
	if err := sm2.Load(); err != nil {
		t.Fatal(err)
	}
	if got := sm2.GetStationSubscriptions("KJAX"); len(got) != 1 {
		t.Errorf("stations not restored: %v", got)
	}
	if got := sm2.GetSAMESubscriptions("012031"); !reflect.DeepEqual(got, []Subscription{{UserID: "u1", Filters: []string{"all"}}}) {
		t.Errorf("SAME subscriptions not restored: %v", got)
	}
	if got := sm2.GetZIPSubscriptions("48103"); !reflect.DeepEqual(got, []Subscription{{UserID: "u1", Filters: []string{"warning"}}}) {
		t.Errorf("ZIP subscriptions not restored: %v", got)
	}
}

func TestLoadLegacyStationOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subs.json")
	legacy := `{"KJAX":[{"UserID":"u1","Filters":["cap"]}],"KTAE":[{"UserID":"u2","Filters":["warning"]}]}`
	if err := os.WriteFile(path, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	sm := NewSubscriptionManager()
	sm.filePath = path
	if err := sm.Load(); err != nil {
		t.Fatalf("Load legacy: %v", err)
	}
	if got := sm.GetStationSubscriptions("KTAE"); len(got) != 1 || got[0].UserID != "u2" {
		t.Errorf("legacy stations not loaded: %v", got)
	}
	if err := sm.Save(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"version": 2`) {
		t.Errorf("save after legacy load should write v2, got: %s", data)
	}
}

func TestLoadCorruptFileFallsBackToBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subs.json")
	os.WriteFile(path, []byte("{not json"), 0644)
	os.WriteFile(path+".backup", []byte(`{"version":2,"stations":{},"same":{"012031":[{"UserID":"u1","Filters":["cap"]}]}}`), 0644)

	sm := NewSubscriptionManager()
	sm.filePath = path
	if err := sm.Load(); err != nil {
		t.Fatal(err)
	}
	if got := sm.GetSAMESubscriptions("012031"); len(got) != 1 {
		t.Errorf("backup not used: %v", got)
	}
}
