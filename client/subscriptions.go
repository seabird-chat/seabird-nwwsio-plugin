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

const MaxRecentMessages = 5

type RecentMessage struct {
	Station   string
	DataType  string
	AwipsID   string
	Issue     string
	Text      string
	Timestamp time.Time
}

type Subscription struct {
	UserID  string
	Filters []string // "cap", "all", or product category names
}

type UserSubscription struct {
	Code    string
	Filters []string
}

// Files without a version key are the original station-only map and are
// migrated on load.
const subscriptionFileVersion = 2

type persistedSubscriptions struct {
	Version  int                       `json:"version"`
	Stations map[string][]Subscription `json:"stations"`
	SAME     map[string][]Subscription `json:"same"`
	ZIP      map[string][]Subscription `json:"zip"`
}

type subscriptionSets struct {
	stations, same, zip map[string][]Subscription
}

type SubscriptionManager struct {
	mu                 sync.RWMutex
	stationSubscribers map[string][]Subscription
	sameSubscribers    map[string][]Subscription
	zipSubscribers     map[string][]Subscription
	recentMessages     map[string][]RecentMessage
	filePath           string
	autoSaveChan       chan struct{}
	stopAutoSave       chan struct{}
}

func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		stationSubscribers: make(map[string][]Subscription),
		sameSubscribers:    make(map[string][]Subscription),
		zipSubscribers:     make(map[string][]Subscription),
		recentMessages:     make(map[string][]RecentMessage),
		autoSaveChan:       make(chan struct{}, 1),
		stopAutoSave:       make(chan struct{}),
	}
}

func (sm *SubscriptionManager) SetPersistenceFile(filePath string) {
	sm.mu.Lock()
	sm.filePath = filePath
	sm.mu.Unlock()

	go sm.autoSaveLoop()

	log.Info().Str("file", filePath).Msg("Subscription persistence enabled")
}

func (sm *SubscriptionManager) Load() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.filePath == "" {
	}

	data, err := os.ReadFile(sm.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info().Str("file", sm.filePath).Msg("No existing subscription file found, starting fresh")
		}
		return fmt.Errorf("failed to read subscriptions file: %w", err)
	}

	sets, err := decodeSubscriptions(data)
	if err != nil {
		return sm.loadBackup(err)
	}
	sm.apply(sets)

	log.Info().
		Str("file", sm.filePath).
		Int("stations", len(sets.stations)).
		Int("station_subscriptions", countSubscriptions(sets.stations)).
		Int("same_codes", len(sets.same)).
		Int("same_subscriptions", countSubscriptions(sets.same)).
		Int("zips", len(sets.zip)).
		Int("zip_subscriptions", countSubscriptions(sets.zip)).
		Msg("Loaded subscriptions from file")

	return nil
}

func decodeSubscriptions(data []byte) (subscriptionSets, error) {
	var sets subscriptionSets
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return sets, err
	}

	if _, versioned := probe["version"]; versioned {
		var file persistedSubscriptions
		if err := json.Unmarshal(data, &file); err != nil {
			return sets, err
		}
		sets = subscriptionSets{stations: file.Stations, same: file.SAME, zip: file.ZIP}
	} else {
		if err := json.Unmarshal(data, &sets.stations); err != nil {
			return sets, err
		}
	}

	for _, m := range []*map[string][]Subscription{&sets.stations, &sets.same, &sets.zip} {
		if *m == nil {
			*m = make(map[string][]Subscription)
		}
	}
	return sets, nil
}

func (sm *SubscriptionManager) apply(sets subscriptionSets) {
	sm.stationSubscribers = sets.stations
	sm.sameSubscribers = sets.same
	sm.zipSubscribers = sets.zip
}

func countSubscriptions(m map[string][]Subscription) int {
	total := 0
	for _, subs := range m {
		total += len(subs)
	}
	return total
}

func (sm *SubscriptionManager) loadBackup(originalErr error) error {
	backupPath := sm.filePath + ".backup"
	data, err := os.ReadFile(backupPath)
	if err != nil {
		log.Error().
			Err(originalErr).
			Str("file", sm.filePath).
			Msg("Subscription file corrupted and no backup available, starting fresh")
	}

	sets, err := decodeSubscriptions(data)
	if err != nil {
		log.Error().
			Err(originalErr).
			Str("file", sm.filePath).
			Msg("Both subscription file and backup are corrupted, starting fresh")
	}
	sm.apply(sets)

	log.Warn().
		Err(originalErr).
		Str("file", sm.filePath).
		Str("backup_file", backupPath).
		Int("stations", len(sets.stations)).
		Int("same_codes", len(sets.same)).
		Int("zips", len(sets.zip)).
		Msg("Loaded subscriptions from backup after main file corruption")

	return nil
}

func (sm *SubscriptionManager) Save() error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.filePath == "" {
		return nil // No persistence configured
	}

	file := persistedSubscriptions{
		Version:  subscriptionFileVersion,
		Stations: sm.stationSubscribers,
		SAME:     sm.sameSubscribers,
		ZIP:      sm.zipSubscribers,
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal subscriptions: %w", err)
	}

	dir := filepath.Dir(sm.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if _, err := os.Stat(sm.filePath); err == nil {
		backupPath := sm.filePath + ".backup"
		if err := copyFile(sm.filePath, backupPath); err != nil {
			log.Warn().Err(err).Msg("Failed to create backup, continuing with save")
		}
	}

	tmpFile := sm.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tmpFile, sm.filePath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	log.Debug().Str("file", sm.filePath).Msg("Saved subscriptions to disk")
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func (sm *SubscriptionManager) triggerAutoSave() {
	select {
	case sm.autoSaveChan <- struct{}{}:
	default:
	}
}

func (sm *SubscriptionManager) autoSaveLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopAutoSave:
			log.Info().Msg("Stopping auto-save goroutine")
			return
		case <-sm.autoSaveChan:
			if err := sm.Save(); err != nil {
				log.Error().Err(err).Msg("Failed to auto-save subscriptions")
			}
		case <-ticker.C:
			if err := sm.Save(); err != nil {
				log.Error().Err(err).Msg("Failed to save subscriptions during periodic backup")
			}
		}
	}
}

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

func normalizeFilters(filters []string) []string {
	if len(filters) == 0 {
		return []string{"cap"}
	}
	normalized := make([]string, len(filters))
	for i, f := range filters {
		normalized[i] = strings.ToLower(f)
	}
	return normalized
}

func upsertSubscription(m map[string][]Subscription, key string, sub Subscription) {
	subs, _ := removeUserSubscription(m[key], sub.UserID)
	m[key] = append(subs, sub)
}

func removeUserSubscription(subs []Subscription, userID string) ([]Subscription, bool) {
	for i, sub := range subs {
		if sub.UserID == userID {
			return append(subs[:i:i], subs[i+1:]...), true
		}
	}
	return subs, false
}

func dropUserSubscription(m map[string][]Subscription, key, userID string) bool {
	subs, found := removeUserSubscription(m[key], userID)
	if !found {
		return false
	}
	if len(subs) == 0 {
		delete(m, key)
	} else {
		m[key] = subs
	}
	return true
}

func dropUserEverywhere(m map[string][]Subscription, userID string) int {
	count := 0
	for key := range m {
		if dropUserSubscription(m, key, userID) {
			count++
		}
	}
	return count
}

func userSubscriptions(m map[string][]Subscription, userID string) []UserSubscription {
	var result []UserSubscription
	for key, subs := range m {
		for _, sub := range subs {
			if sub.UserID == userID {
				result = append(result, UserSubscription{Code: key, Filters: sub.Filters})
				break
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Code < result[j].Code })
	return result
}

func copySubscriptions(subs []Subscription) []Subscription {
	result := make([]Subscription, len(subs))
	copy(result, subs)
	return result
}

func (sm *SubscriptionManager) SubscribeToStation(userID, stationCode string, filters []string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	upsertSubscription(sm.stationSubscribers, strings.ToUpper(stationCode), Subscription{
		UserID:  userID,
		Filters: normalizeFilters(filters),
	})

	sm.triggerAutoSave()
}

func (sm *SubscriptionManager) UnsubscribeFromStation(userID, stationCode string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !dropUserSubscription(sm.stationSubscribers, strings.ToUpper(stationCode), userID) {
		return false
	}
	sm.triggerAutoSave()
	return true
}

func (sm *SubscriptionManager) GetStationSubscriptions(stationCode string) []Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return copySubscriptions(sm.stationSubscribers[strings.ToUpper(stationCode)])
}

func (sm *SubscriptionManager) GetUserStations(userID string) []UserSubscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return userSubscriptions(sm.stationSubscribers, userID)
}

func (sm *SubscriptionManager) SubscribeToSAME(userID string, codes []string, filters []string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	normalized := normalizeFilters(filters)
	for _, code := range codes {
		upsertSubscription(sm.sameSubscribers, code, Subscription{
			UserID:  userID,
			Filters: append([]string(nil), normalized...),
		})
	}

	sm.triggerAutoSave()
}

func (sm *SubscriptionManager) UnsubscribeFromSAME(userID string, codes []string) []string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var removed []string
	for _, code := range codes {
		if dropUserSubscription(sm.sameSubscribers, code, userID) {
			removed = append(removed, code)
		}
	}
	if len(removed) > 0 {
		sm.triggerAutoSave()
	}
	return removed
}

func (sm *SubscriptionManager) UnsubscribeFromAllSAME(userID string) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	count := dropUserEverywhere(sm.sameSubscribers, userID)
	if count > 0 {
		sm.triggerAutoSave()
	}
	return count
}

func (sm *SubscriptionManager) GetSAMESubscriptions(code string) []Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return copySubscriptions(sm.sameSubscribers[code])
}

func (sm *SubscriptionManager) GetUserSAMESubscriptions(userID string) []UserSubscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return userSubscriptions(sm.sameSubscribers, userID)
}

func (sm *SubscriptionManager) SubscribeToZIP(userID string, zips []string, filters []string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	normalized := normalizeFilters(filters)
	for _, zip := range zips {
		upsertSubscription(sm.zipSubscribers, zip, Subscription{
			UserID:  userID,
			Filters: append([]string(nil), normalized...),
		})
	}

	sm.triggerAutoSave()
}

func (sm *SubscriptionManager) UnsubscribeFromZIP(userID string, zips []string) []string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var removed []string
	for _, zip := range zips {
		if dropUserSubscription(sm.zipSubscribers, zip, userID) {
			removed = append(removed, zip)
		}
	}
	if len(removed) > 0 {
		sm.triggerAutoSave()
	}
	return removed
}

func (sm *SubscriptionManager) UnsubscribeFromAllZIP(userID string) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	count := dropUserEverywhere(sm.zipSubscribers, userID)
	if count > 0 {
		sm.triggerAutoSave()
	}
	return count
}

func (sm *SubscriptionManager) GetZIPSubscriptions(zip string) []Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return copySubscriptions(sm.zipSubscribers[zip])
}

func (sm *SubscriptionManager) GetAllZIPSubscriptions() map[string][]Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string][]Subscription, len(sm.zipSubscribers))
	for zip, subs := range sm.zipSubscribers {
		result[zip] = copySubscriptions(subs)
	}
	return result
}

func (sm *SubscriptionManager) GetUserZIPSubscriptions(userID string) []UserSubscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return userSubscriptions(sm.zipSubscribers, userID)
}

func (sm *SubscriptionManager) UnsubscribeFromAll(userID string) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	count := dropUserEverywhere(sm.stationSubscribers, userID) +
		dropUserEverywhere(sm.sameSubscribers, userID) +
		dropUserEverywhere(sm.zipSubscribers, userID)
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

func ValidateFilters(filters []string) (invalidFilters []string) {
	if len(filters) == 0 {
		return nil
	}

	validFilters := make(map[string]bool)
	validFilters["all"] = true
	validFilters["cap"] = true

	for _, category := range nwwsio.GetAllCategories() {
		validFilters[strings.ToLower(category)] = true
	}

	for _, filter := range filters {
		normalized := strings.ToLower(strings.TrimSpace(filter))
		if !validFilters[normalized] {
			invalidFilters = append(invalidFilters, filter)
		}
	}

	return invalidFilters
}

func GetValidFilters() []string {
	filters := []string{"all", "cap"}
	categories := nwwsio.GetAllCategories()
	sort.Strings(categories)
	return append(filters, categories...)
}
