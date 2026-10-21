package mailrules

import (
	"os"
	"path/filepath"
)

// Keep this inode in the config directory, outside the encryption and cache
// inventories. Removing or atomically replacing a locked file splits the lock.
// Closing the descriptor releases the OS lock, including on process exit.
func lockProgress(dir string) (*os.File, bool, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, false, err
	}
	file, err := os.OpenFile(filepath.Join(dir, "mail_rules.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false, err
	}
	if err := file.Chmod(0600); err != nil {
		file.Close() //nolint:errcheck
		return nil, false, err
	}
	acquired, err := tryLockFile(file)
	if !acquired || err != nil {
		file.Close() //nolint:errcheck
		return nil, false, err
	}
	return file, true, nil
}
