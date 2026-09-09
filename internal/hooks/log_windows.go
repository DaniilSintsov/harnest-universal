//go:build windows

package hooks

import (
	"fmt"
	"os"
)

func openLogFile(_ *os.Root, _ string) (*os.File, error) {
	return nil, fmt.Errorf("native hooks are not supported on Windows")
}
