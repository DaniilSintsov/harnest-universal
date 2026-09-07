//go:build !windows

package hooks

import (
	"fmt"
	"os"
	"syscall"
)

func openLogFile(root *os.Root, path string) (*os.File, error) {
	before, err := root.Lstat(path)
	flags := os.O_APPEND | os.O_WRONLY | syscall.O_NONBLOCK
	if os.IsNotExist(err) {
		// Exclusive creation cannot follow a link appearing after Lstat.
		return root.OpenFile(path, flags|os.O_CREATE|os.O_EXCL, 0600)
	}
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("hook log must be a regular file")
	}
	file, err := root.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	// os.Root follows in-root symlinks. Check the opened inode before writing or
	// chmod, so replacing the final component cannot mutate a different file.
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		file.Close()
		return nil, fmt.Errorf("hook log changed while opening")
	}
	return file, nil
}
