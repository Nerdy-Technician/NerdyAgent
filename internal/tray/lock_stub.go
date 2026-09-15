//go:build !linux

package tray

import "os"

func acquireSingleton() (*os.File, error) {
	return nil, nil
}
