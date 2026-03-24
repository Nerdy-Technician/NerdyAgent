//go:build windows

package sysinfo

import (
	"strconv"
	"strings"
)

func getDiskUsagePercent(path string) float64 {
	device := "C:"
	if strings.TrimSpace(path) != "" && strings.HasPrefix(strings.TrimSpace(path), "D:") {
		device = "D:"
	}
	raw := psOutput(`$d=Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='` + device + `'" -ErrorAction SilentlyContinue; if($d -and $d.Size -gt 0){[double](($d.Size-$d.FreeSpace)*100/$d.Size)} else {0}`)
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return v
}

func getDiskTotals(path string) (int64, int64) {
	device := "C:"
	if strings.TrimSpace(path) != "" && strings.HasPrefix(strings.TrimSpace(path), "D:") {
		device = "D:"
	}
	totalRaw := psOutput(`$d=Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='` + device + `'" -ErrorAction SilentlyContinue; if($d){[int64]$d.Size} else {0}`)
	freeRaw := psOutput(`$d=Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='` + device + `'" -ErrorAction SilentlyContinue; if($d){[int64]$d.FreeSpace} else {0}`)
	total, _ := strconv.ParseInt(strings.TrimSpace(totalRaw), 10, 64)
	free, _ := strconv.ParseInt(strings.TrimSpace(freeRaw), 10, 64)
	if total <= 0 {
		return 0, -1
	}
	return total, free
}
