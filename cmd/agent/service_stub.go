//go:build !windows

package main

func maybeRunAsWindowsService(string) (bool, error) {
	return false, nil
}
