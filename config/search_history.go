package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const searchHistoryFileName = "search_history.json"

// searchHistoryLimit is how many recent searches are kept.
const searchHistoryLimit = 50

// SearchHistoryFile keeps the user's recent search queries, newest first, in
// the cache directory. It follows the encryption setting like other cache files.
type SearchHistoryFile struct{}

func searchHistoryPath() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, searchHistoryFileName), nil
}

// Searches returns the saved queries, newest first. A missing file means none.
func (SearchHistoryFile) Searches() ([]string, error) {
	path, err := searchHistoryPath()
	if err != nil {
		return nil, err
	}
	data, err := SecureReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read search history: %w", err)
	}
	var searches []string
	if err := json.Unmarshal(data, &searches); err != nil {
		// Decoder errors can quote the saved queries; do not return them.
		return nil, errors.New("search history file contains invalid JSON")
	}
	return searches, nil
}

// Add saves query as the newest search. A query searched before moves to the
// top instead of being saved twice, the oldest searches beyond 50 are dropped,
// and blank queries are not saved. It returns the saved queries.
func (h SearchHistoryFile) Add(query string) ([]string, error) {
	searches, err := h.Searches()
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return searches, nil
	}
	updated := make([]string, 0, min(len(searches)+1, searchHistoryLimit))
	updated = append(updated, query)
	for _, saved := range searches {
		if len(updated) == searchHistoryLimit {
			break
		}
		if saved != query {
			updated = append(updated, saved)
		}
	}
	return updated, h.save(updated)
}

// Remove deletes query from the saved searches and returns the rest.
func (h SearchHistoryFile) Remove(query string) ([]string, error) {
	searches, err := h.Searches()
	if err != nil {
		return nil, err
	}
	updated := make([]string, 0, len(searches))
	for _, saved := range searches {
		if saved != query {
			updated = append(updated, saved)
		}
	}
	return updated, h.save(updated)
}

// Clear deletes all saved searches.
func (SearchHistoryFile) Clear() error {
	path, err := searchHistoryPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear search history: %w", err)
	}
	return nil
}

func (SearchHistoryFile) save(searches []string) error {
	path, err := searchHistoryPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(searches, "", "  ")
	if err != nil {
		return err
	}
	if err := AtomicSecureWriteFile(path, data); err != nil {
		return fmt.Errorf("save search history: %w", err)
	}
	return nil
}
