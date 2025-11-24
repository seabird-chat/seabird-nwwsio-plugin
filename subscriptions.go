package client

import (
	"strings"
	"sync"
)

type SubscriptionManager struct {
	mu                sync.RWMutex
	stationSubscribers map[string][]string // station code -> list of user IDs
	zipSubscribers    map[string][]string // zip code -> list of user IDs
}

func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		stationSubscribers: make(map[string][]string),
		zipSubscribers:    make(map[string][]string),
	}
}

func (sm *SubscriptionManager) SubscribeToStation(userID, stationCode string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stationCode = strings.ToUpper(stationCode)

	if !contains(sm.stationSubscribers[stationCode], userID) {
		sm.stationSubscribers[stationCode] = append(sm.stationSubscribers[stationCode], userID)
	}
}

func (sm *SubscriptionManager) UnsubscribeFromStation(userID, stationCode string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stationCode = strings.ToUpper(stationCode)

	subscribers := sm.stationSubscribers[stationCode]
	for i, id := range subscribers {
		if id == userID {
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
	subscribers := sm.stationSubscribers[stationCode]

	result := make([]string, len(subscribers))
	copy(result, subscribers)
	return result
}

func (sm *SubscriptionManager) GetUserStationSubscriptions(userID string) []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var stations []string
	for station, subscribers := range sm.stationSubscribers {
		if contains(subscribers, userID) {
			stations = append(stations, station)
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

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
