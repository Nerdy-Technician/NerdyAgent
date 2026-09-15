//go:build !linux

package tray

import (
	"fmt"
	"os"
)

// Run is only implemented on Linux (StatusNotifier / AppIndicator).
func Run() error {
	fmt.Fprintln(os.Stderr, "NerdyRMM tray is supported on Linux only")
	return fmt.Errorf("tray not supported on this platform")
}
