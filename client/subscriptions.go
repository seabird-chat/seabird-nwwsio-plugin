package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
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
	filePath           string                     // path to persistence file
	autoSaveChan       chan struct{}              // signal channel for auto-save
	stopAutoSave       chan struct{}              // signal to stop auto-save goroutine
}

func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		stationSubscribers: make(map[string][]Subscription),
		recentMessages:     make(map[string][]RecentMessage),
		autoSaveChan:       make(chan struct{}, 1),
		stopAutoSave:       make(chan struct{}),
	}
}

// SetPersistenceFile sets the file path for persistence and starts the auto-save goroutine
func (sm *SubscriptionManager) SetPersistenceFile(filePath string) {
	sm.mu.Lock()
	sm.filePath = filePath
	sm.mu.Unlock()

	// Start auto-save goroutine
	go sm.autoSaveLoop()

	log.Info().Str("file", filePath).Msg("Subscription persistence enabled")
}

// Load reads subscriptions from the persistence file
func (sm *SubscriptionManager) Load() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.filePath == "" {
		return nil // No persistence configured
	}

	data, err := os.ReadFile(sm.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info().Str("file", sm.filePath).Msg("No existing subscription file found, starting fresh")
			return nil // No file yet, that's okay
		}
		return fmt.Errorf("failed to read subscriptions file: %w", err)
	}

	// Try to unmarshal
	var subscriptions map[string][]Subscription
	if err := json.Unmarshal(data, &subscriptions); err != nil {
		// File is corrupted, try to recover by loading backup
		return sm.loadBackup(err)
	}

	sm.stationSubscribers = subscriptions

	// Count total subscriptions
	totalSubs := 0
	for _, subs := range subscriptions {
		totalSubs += len(subs)
	}

	log.Info().
		Str("file", sm.filePath).
		Int("stations", len(subscriptions)).
		Int("total_subscriptions", totalSubs).
		Msg("Loaded subscriptions from file")

	return nil
}

// loadBackup attempts to load from a backup file if the main file is corrupted
func (sm *SubscriptionManager) loadBackup(originalErr error) error {
	backupPath := sm.filePath + ".backup"
	data, err := os.ReadFile(backupPath)
	if err != nil {
		log.Error().
			Err(originalErr).
			Str("file", sm.filePath).
			Msg("Subscription file corrupted and no backup available, starting fresh")
		return nil // Start fresh if backup also doesn't exist
	}

	var subscriptions map[string][]Subscription
	if err := json.Unmarshal(data, &subscriptions); err != nil {
		log.Error().
			Err(originalErr).
			Str("file", sm.filePath).
			Msg("Both subscription file and backup are corrupted, starting fresh")
		return nil // Start fresh if backup is also corrupted
	}

	sm.stationSubscribers = subscriptions
	log.Warn().
		Err(originalErr).
		Str("file", sm.filePath).
		Str("backup_file", backupPath).
		Int("stations", len(subscriptions)).
		Msg("Loaded subscriptions from backup after main file corruption")

	return nil
}

// Save writes subscriptions to disk atomically
func (sm *SubscriptionManager) Save() error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.filePath == "" {
		return nil // No persistence configured
	}

	// Marshal the subscriptions
	data, err := json.MarshalIndent(sm.stationSubscribers, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal subscriptions: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(sm.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create backup of existing file before overwriting
	if _, err := os.Stat(sm.filePath); err == nil {
		backupPath := sm.filePath + ".backup"
		if err := copyFile(sm.filePath, backupPath); err != nil {
			log.Warn().Err(err).Msg("Failed to create backup, continuing with save")
		}
	}

	// Atomic write: write to temp file, then rename
	tmpFile := sm.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Rename is atomic on POSIX systems
	if err := os.Rename(tmpFile, sm.filePath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	log.Debug().Str("file", sm.filePath).Msg("Saved subscriptions to disk")
	return nil
}

// copyFile creates a copy of a file
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// triggerAutoSave signals the auto-save goroutine to save (non-blocking)
func (sm *SubscriptionManager) triggerAutoSave() {
	select {
	case sm.autoSaveChan <- struct{}{}:
	default:
		// Channel full, save already pending
	}
}

// autoSaveLoop runs in a goroutine and handles periodic saves
func (sm *SubscriptionManager) autoSaveLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopAutoSave:
			log.Info().Msg("Stopping auto-save goroutine")
			return
		case <-sm.autoSaveChan:
			// Immediate save requested
			if err := sm.Save(); err != nil {
				log.Error().Err(err).Msg("Failed to auto-save subscriptions")
			}
		case <-ticker.C:
			// Periodic backup save
			if err := sm.Save(); err != nil {
				log.Error().Err(err).Msg("Failed to save subscriptions during periodic backup")
			}
		}
	}
}

// Close stops the auto-save goroutine and performs a final save
func (sm *SubscriptionManager) Close() error {
	close(sm.stopAutoSave)
	return sm.Save()
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

	sm.triggerAutoSave()
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

			sm.triggerAutoSave()
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

	for station := range sm.stationSubscribers {
		subscriptions := sm.stationSubscribers[station]
		newSubs := make([]Subscription, 0, len(subscriptions))

		for _, sub := range subscriptions {
			if sub.UserID == userID {
				count++
			} else {
				newSubs = append(newSubs, sub)
			}
		}

		if len(newSubs) == 0 {
			delete(sm.stationSubscribers, station)
		} else if len(newSubs) != len(subscriptions) {
			sm.stationSubscribers[station] = newSubs
		}
	}

	if count > 0 {
		sm.triggerAutoSave()
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
