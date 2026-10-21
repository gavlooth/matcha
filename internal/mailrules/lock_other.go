//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package mailrules

import (
	"errors"
	"os"
)

func tryLockFile(*os.File) (bool, error) {
	return false, errors.New("local mail rules need OS advisory file locking on this platform")
}
