//go:build windows

package managedfile

import "os"

// os.Rename uses MoveFileEx with replacement semantics on Windows. The caller
// has already flushed and closed the temporary file before this operation.
func replaceFile(source, target string) error {
	return os.Rename(source, target)
}
