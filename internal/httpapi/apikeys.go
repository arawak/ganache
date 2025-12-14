package httpapi

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type APIKey struct {
	ID          string   `yaml:"id"`
	Key         string   `yaml:"key"`
	Permissions []string `yaml:"permissions"`
}

type APIKeyStore struct {
	byKey map[string]*APIKey
}

func LoadAPIKeys(path string) (*APIKeyStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read api keys file: %w", err)
	}

	var entries []APIKey
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse api keys file: %w", err)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("api keys file is empty")
	}

	store := &APIKeyStore{byKey: make(map[string]*APIKey, len(entries))}
	for i := range entries {
		entry := entries[i]
		entry.ID = strings.TrimSpace(entry.ID)
		entry.Key = strings.TrimSpace(entry.Key)
		if entry.ID == "" {
			return nil, fmt.Errorf("api key at index %d has empty id", i)
		}
		if entry.Key == "" {
			return nil, fmt.Errorf("api key %q has empty key", entry.ID)
		}
		if len(entry.Permissions) == 0 {
			return nil, fmt.Errorf("api key %q has no permissions", entry.ID)
		}
		if _, exists := store.byKey[entry.Key]; exists {
			return nil, fmt.Errorf("duplicate api key value for id %q", entry.ID)
		}
		// store pointer to normalized entry
		entries[i] = entry
		store.byKey[entry.Key] = &entries[i]
	}

	return store, nil
}

func (s *APIKeyStore) Lookup(key string) (*APIKey, bool) {
	if s == nil {
		return nil, false
	}
	k, ok := s.byKey[key]
	return k, ok
}
