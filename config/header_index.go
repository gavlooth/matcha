package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// headerIndexDirName is listed in cacheDirectories, so enabling encryption
// re-encrypts these files. That walk is not recursive, so every index file
// sits directly in this directory, named "<account id>.<folder>.json".
const headerIndexDirName = "header_index"

// HeaderIndex is the saved copy of every email header in one folder of one
// account, used to search mail on this computer. UIDValidity is the folder's
// IMAP UIDVALIDITY when the headers were fetched; a different value on the
// server means the UIDs changed and the index must be fetched again.
// OtherUIDs are messages in the folder that name none of the account's
// addresses; they are kept so they are not fetched again. AddressSettings
// describes the account's address settings the folder was sorted with; when
// the user changes them the folder is fetched again, so mail hidden before
// can show up and mail that no longer matches goes away.
type HeaderIndex struct {
	UIDValidity     uint32        `json:"uid_validity"`
	AddressSettings string        `json:"address_settings,omitempty"`
	Emails          []CachedEmail `json:"emails"`
	OtherUIDs       []uint32      `json:"other_uids,omitempty"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

func headerIndexDir() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, headerIndexDirName), nil
}

// headerIndexFilePrefix is the file name start shared by all index files of
// one account. Account IDs are UUIDs, so they never contain the dot.
func headerIndexFilePrefix(accountID string) string {
	return sanitizeFolderName(accountID) + "."
}

func headerIndexFile(accountID, folderName string) (string, error) {
	dir, err := headerIndexDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, headerIndexFilePrefix(accountID)+sanitizeFolderName(folderName)+".json"), nil
}

// SaveHeaderIndex writes the header index of one account folder. It goes
// through SecureWriteFile, so it is encrypted when cache encryption is on.
func SaveHeaderIndex(accountID, folderName string, index *HeaderIndex) error {
	path, err := headerIndexFile(accountID, folderName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	index.UpdatedAt = time.Now()
	data, err := json.Marshal(index)
	if err != nil {
		return err
	}
	return SecureWriteFile(path, data, 0600)
}

// LoadHeaderIndex reads the header index of one account folder. A folder that
// was never indexed returns an empty index and no error.
func LoadHeaderIndex(accountID, folderName string) (*HeaderIndex, error) {
	path, err := headerIndexFile(accountID, folderName)
	if err != nil {
		return nil, err
	}
	data, err := SecureReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &HeaderIndex{}, nil
	}
	if err != nil {
		return nil, err
	}
	var index HeaderIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}
	return &index, nil
}

// RemoveAccountHeaderIndex deletes every header index file of the account.
func RemoveAccountHeaderIndex(accountID string) error {
	dir, err := headerIndexDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	prefix := headerIndexFilePrefix(accountID)
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
