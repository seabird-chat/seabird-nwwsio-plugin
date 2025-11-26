package client

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	nwwsio "github.com/seabird-chat/seabird-nwwsio-plugin/internal"
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
	recentMessages     map[string][]RecentMessage // station code -> recent messages (last 5)
}

func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		stationSubscribers: make(map[string][]Subscription),
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

	return count
}

func (sm *SubscriptionManager) AddRecentMessage(msg RecentMessage) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	station := strings.ToUpper(msg.Station)
	messages := sm.recentMessages[station]

	messages = append(messages, msg)
	if len(messages) > MaxRecentMessages {
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

// ValidateFilters validates that all provided filters are either special filters or known product categories
func ValidateFilters(filters []string) (invalidFilters []string) {
	if len(filters) == 0 {
		return nil
	}

	// Build set of valid filters
	validFilters := make(map[string]bool)
	validFilters["all"] = true
	validFilters["cap"] = true

	// Add all known product categories (case-insensitive)
	for _, category := range nwwsio.GetAllCategories() {
		validFilters[strings.ToLower(category)] = true
	}

	// Check each provided filter
	for _, filter := range filters {
		normalized := strings.ToLower(strings.TrimSpace(filter))
		if !validFilters[normalized] {
			invalidFilters = append(invalidFilters, filter)
		}
	}

	return invalidFilters
}

// GetValidFilters returns a sorted list of all valid filter options
func GetValidFilters() []string {
	filters := []string{"all", "cap"}
	categories := nwwsio.GetAllCategories()
	sort.Strings(categories)
	return append(filters, categories...)
}
