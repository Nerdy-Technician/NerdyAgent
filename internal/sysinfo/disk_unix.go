//go:build !windows

package sysinfo

import "syscall"

func getDiskUsagePercent(path string) float64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	total := float64(st.Blocks) * float64(st.Bsize)
	free := float64(st.Bavail) * float64(st.Bsize)
	if total == 0 {
		return 0
	}
	used := total - free
	return used * 100 / total
}

func getDiskTotals(path string) (int64, int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, -1
	}
	total := int64(st.Blocks) * int64(st.Bsize)
	free := int64(st.Bavail) * int64(st.Bsize)
	return total, free
}
