package client

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type RecentMessage struct {
	Station   string
	DataType  string
	AwipsID   string
	Issue     string
	Text      string
	Timestamp time.Time
}

// Subscription represents a user's subscription to a station with filtering
type Subscription struct {
	UserID  string
	Filters []string // Filters: "cap", "all", or category names (Aviation, Hydrology, Marine, etc.)
}

type SubscriptionManager struct {
	mu                 sync.RWMutex
	stationSubscribers map[string][]Subscription  // station code -> list of subscriptions
	zipSubscribers     map[string][]string        // zip code -> list of user IDs
	recentMessages     map[string][]RecentMessage // station code -> recent messages (last 5)
}

func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		stationSubscribers: make(map[string][]Subscription),
		zipSubscribers:     make(map[string][]string),
		recentMessages:     make(map[string][]RecentMessage),
	}
}

func ValidateStationCode(code string) error {
	code = strings.ToUpper(code)
	if len(code) != 4 {
		return fmt.Errorf("station code must be 4 characters (e.g., KARB)")
	}
	if code[0] != 'K' && code[0] != 'P' {
		return fmt.Errorf("station code must start with K or P")
	}
	return nil
}

func (sm *SubscriptionManager) SubscribeToStation(userID, stationCode string, filters []string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stationCode = strings.ToUpper(stationCode)

	// Default to "cap" if no filters provided
	if len(filters) == 0 {
		filters = []string{"cap"}
	}

	// Normalize filter values to lowercase
	normalizedFilters := make([]string, len(filters))
	for i, f := range filters {
		normalizedFilters[i] = strings.ToLower(f)
	}

	// Remove existing subscription if present
	subs := sm.stationSubscribers[stationCode]
	for i, sub := range subs {
		if sub.UserID == userID {
			sm.stationSubscribers[stationCode] = append(subs[:i], subs[i+1:]...)
			break
		}
	}

	// Add new subscription with filters
	sm.stationSubscribers[stationCode] = append(sm.stationSubscribers[stationCode], Subscription{
		UserID:  userID,
		Filters: normalizedFilters,
	})
}

func (sm *SubscriptionManager) UnsubscribeFromStation(userID, stationCode string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stationCode = strings.ToUpper(stationCode)

	subscribers := sm.stationSubscribers[stationCode]
	for i, sub := range subscribers {
		if sub.UserID == userID {
			sm.stationSubscribers[stationCode] = append(subscribers[:i], subscribers[i+1:]...)
			if len(sm.stationSubscribers[stationCode]) == 0 {
				delete(sm.stationSubscribers, stationCode)
			}
			return true
		}
	}
	return false
}

func (sm *SubscriptionManager) SubscribeToZip(userID, zipCode string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !contains(sm.zipSubscribers[zipCode], userID) {
		sm.zipSubscribers[zipCode] = append(sm.zipSubscribers[zipCode], userID)
	}
}

func (sm *SubscriptionManager) UnsubscribeFromZip(userID, zipCode string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	subscribers := sm.zipSubscribers[zipCode]
	for i, id := range subscribers {
		if id == userID {
			sm.zipSubscribers[zipCode] = append(subscribers[:i], subscribers[i+1:]...)
			if len(sm.zipSubscribers[zipCode]) == 0 {
				delete(sm.zipSubscribers, zipCode)
			}
			return true
		}
	}
	return false
}

func (sm *SubscriptionManager) GetStationSubscribers(stationCode string) []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stationCode = strings.ToUpper(stationCode)
	subscriptions := sm.stationSubscribers[stationCode]

	result := make([]string, len(subscriptions))
	for i, sub := range subscriptions {
		result[i] = sub.UserID
	}
	return result
}

// GetStationSubscriptions returns all subscriptions (with filters) for a station
func (sm *SubscriptionManager) GetStationSubscriptions(stationCode string) []Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stationCode = strings.ToUpper(stationCode)
	subscriptions := sm.stationSubscribers[stationCode]

	result := make([]Subscription, len(subscriptions))
	copy(result, subscriptions)
	return result
}

func (sm *SubscriptionManager) GetUserStationSubscriptions(userID string) []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var stations []string
	for station, subscriptions := range sm.stationSubscribers {
		for _, sub := range subscriptions {
			if sub.UserID == userID {
				stations = append(stations, station)
				break
			}
		}
	}
	return stations
}

func (sm *SubscriptionManager) GetUserZipSubscriptions(userID string) []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var zips []string
	for zip, subscribers := range sm.zipSubscribers {
		if contains(subscribers, userID) {
			zips = append(zips, zip)
		}
	}
	return zips
}

func (sm *SubscriptionManager) UnsubscribeFromAll(userID string) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	count := 0

	for station, subscriptions := range sm.stationSubscribers {
		for i, sub := range subscriptions {
			if sub.UserID == userID {
				sm.stationSubscribers[station] = append(subscriptions[:i], subscriptions[i+1:]...)
				if len(sm.stationSubscribers[station]) == 0 {
					delete(sm.stationSubscribers, station)
				}
				count++
				break
			}
		}
	}

	for zip, subscribers := range sm.zipSubscribers {
		for i, id := range subscribers {
			if id == userID {
				sm.zipSubscribers[zip] = append(subscribers[:i], subscribers[i+1:]...)
				if len(sm.zipSubscribers[zip]) == 0 {
					delete(sm.zipSubscribers, zip)
				}
				count++
				break
			}
		}
	}

	return count
}

func (sm *SubscriptionManager) AddRecentMessage(msg RecentMessage) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	station := strings.ToUpper(msg.Station)
	messages := sm.recentMessages[station]

	messages = append(messages, msg)
	if len(messages) > 5 {
		messages = messages[1:]
	}

	sm.recentMessages[station] = messages
}

func (sm *SubscriptionManager) GetRecentMessages(stationCode string) []RecentMessage {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stationCode = strings.ToUpper(stationCode)
	messages := sm.recentMessages[stationCode]

	result := make([]RecentMessage, len(messages))
	copy(result, messages)
	return result
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
