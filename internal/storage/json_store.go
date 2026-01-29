package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/noahxzhu/pushover-notify/internal/model"
)

type Store struct {
	mu             sync.RWMutex
	filePath       string
	Data           *model.AppSchema
	lastLoadedTime time.Time
}

func NewStore(filePath string) *Store {
	return &Store{
		filePath: filePath,
		Data: &model.AppSchema{
			Settings:      model.Settings{},
			Notifications: []*model.Notification{},
		},
	}
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, err := os.Stat(s.filePath)
	if err == nil {
		s.lastLoadedTime = info.ModTime()
	}

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.Data = &model.AppSchema{
				Settings:      model.Settings{RepeatTimes: 3, RepeatInterval: "30m"},
				Notifications: []*model.Notification{},
			}
			return nil
		}
		return fmt.Errorf("failed to read file: %w", err)
	}

	if len(data) == 0 {
		s.Data = &model.AppSchema{
			Settings:      model.Settings{RepeatTimes: 3, RepeatInterval: "30m"},
			Notifications: []*model.Notification{},
		}
		return nil
	}

	if err := json.Unmarshal(data, &s.Data); err != nil {
		// Attempt migration from old []Notification format
		var oldNotifs []*model.Notification
		if err2 := json.Unmarshal(data, &oldNotifs); err2 == nil {
			s.Data = &model.AppSchema{
				Settings:      model.Settings{RepeatTimes: 3, RepeatInterval: "30m"},
				Notifications: oldNotifs,
			}
			return nil
		}

		return fmt.Errorf("failed to unmarshal data: %w", err)
	}

	// Set defaults
	if s.Data.Settings.RepeatTimes == 0 {
		s.Data.Settings.RepeatTimes = 3
	}
	if s.Data.Settings.RepeatInterval == "" {
		s.Data.Settings.RepeatInterval = "30m"
	}
	if s.Data.Notifications == nil {
		s.Data.Notifications = []*model.Notification{}
	}

	// Migration/Defaults for legacy data
	// If RepeatTimes is 0 or RepeatInterval is empty, assume legacy and use current settings (or defaults)
	// Note: This treats intentional "0 retries" as "use default" for existing data, which is acceptable for migration.
	// For new data, we will likely enforce > 0 or handle logic in worker.
	globalRepeatTimes := s.Data.Settings.RepeatTimes
	globalRepeatInterval := s.Data.Settings.RepeatInterval

	// Ensure Global defaults if they were somehow 0/empty
	if globalRepeatTimes == 0 {
		globalRepeatTimes = 3
	}
	if globalRepeatInterval == "" {
		globalRepeatInterval = "30m"
	}

	for _, n := range s.Data.Notifications {
		if n.RepeatTimes == 0 {
			n.RepeatTimes = globalRepeatTimes
		}
		if n.RepeatInterval == "" {
			n.RepeatInterval = globalRepeatInterval
		}
	}

	return nil
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create storage directory: %w", err)
	}

	if err := os.WriteFile(s.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	// Update lastLoadedTime so we don't reload our own change
	if info, err := os.Stat(s.filePath); err == nil {
		s.lastLoadedTime = info.ModTime()
	}

	return nil
}

func (s *Store) CheckDiskChanges() {
	info, err := os.Stat(s.filePath)
	if err != nil {
		return
	}

	s.mu.RLock()
	needsReload := info.ModTime().After(s.lastLoadedTime)
	s.mu.RUnlock()

	if needsReload {
		// This will lock and reload
		s.Load()
	}
}

func (s *Store) AddNotification(n *model.Notification) error {
	s.mu.Lock()
	s.Data.Notifications = append(s.Data.Notifications, n)
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) UpdateSettings(settings model.Settings) error {
	s.mu.Lock()
	s.Data.Settings = settings
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) GetSettings() model.Settings {
	s.CheckDiskChanges()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Data.Settings
}

func (s *Store) GetAllNotifications() []*model.Notification {
	s.CheckDiskChanges()
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*model.Notification, len(s.Data.Notifications))
	copy(result, s.Data.Notifications)
	return result
}

func (s *Store) GetPending() []*model.Notification {
	s.CheckDiskChanges()
	s.mu.RLock()
	defer s.mu.RUnlock()

	var pending []*model.Notification
	for _, n := range s.Data.Notifications {
		if n.Status != model.StatusDone {
			pending = append(pending, n)
		}
	}
	return pending
}

func (s *Store) GetNotification(id string) (*model.Notification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, n := range s.Data.Notifications {
		if n.ID == id {
			// Return copy? Or pointer? Pointer is risky if modified outside lock, but currently we modify in handlers heavily.
			// Safe approach: return the pointer but modifications should ideally be via Update.
			// However, our UpdateNotification implementation will replace values.
			return n, nil
		}
	}
	return nil, fmt.Errorf("notification not found")
}

func (s *Store) UpdateNotification(updated *model.Notification) error {
	s.mu.Lock()
	found := false
	for i, n := range s.Data.Notifications {
		if n.ID == updated.ID {
			s.Data.Notifications[i] = updated
			found = true
			break
		}
	}
	s.mu.Unlock()

	if !found {
		return fmt.Errorf("notification not found")
	}
	return s.Save()
}

func (s *Store) DeleteNotification(id string) error {
	s.mu.Lock()
	found := false
	for i, n := range s.Data.Notifications {
		if n.ID == id {
			s.Data.Notifications = append(s.Data.Notifications[:i], s.Data.Notifications[i+1:]...)
			found = true
			break
		}
	}
	s.mu.Unlock()

	if !found {
		return fmt.Errorf("notification not found")
	}
	return s.Save()
}
