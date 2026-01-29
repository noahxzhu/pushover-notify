package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/noahxzhu/pushover-notify/internal/model"
)

type Store struct {
	mu       sync.RWMutex
	filePath string
	Data     *model.AppSchema
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

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.Data = &model.AppSchema{
				Settings:      model.Settings{MaxRetries: 3, RetryInterval: "30m"},
				Notifications: []*model.Notification{},
			}
			return nil
		}
		return fmt.Errorf("failed to read file: %w", err)
	}

	if len(data) == 0 {
		s.Data = &model.AppSchema{
			Settings:      model.Settings{MaxRetries: 3, RetryInterval: "30m"},
			Notifications: []*model.Notification{},
		}
		return nil
	}

	if err := json.Unmarshal(data, &s.Data); err != nil {
		// Attempt migration from old []Notification format
		var oldNotifs []*model.Notification
		if err2 := json.Unmarshal(data, &oldNotifs); err2 == nil {
			s.Data = &model.AppSchema{
				Settings:      model.Settings{MaxRetries: 3, RetryInterval: "30m"},
				Notifications: oldNotifs,
			}
			return nil
		}

		return fmt.Errorf("failed to unmarshal data: %w", err)
	}

	// Set defaults
	if s.Data.Settings.MaxRetries == 0 {
		s.Data.Settings.MaxRetries = 3
	}
	if s.Data.Settings.RetryInterval == "" {
		s.Data.Settings.RetryInterval = "30m"
	}
	if s.Data.Notifications == nil {
		s.Data.Notifications = []*model.Notification{}
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
	return nil
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Data.Settings
}

func (s *Store) GetAllNotifications() []*model.Notification {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*model.Notification, len(s.Data.Notifications))
	copy(result, s.Data.Notifications)
	return result
}

func (s *Store) GetPending() []*model.Notification {
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
