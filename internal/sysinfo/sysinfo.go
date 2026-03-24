package sysinfo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type softwareEntry struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

var softwareInventoryCache struct {
	mu        sync.Mutex
	scannedAt time.Time
	json      string
	count     int
}

var publicIPCache struct {
	mu       sync.Mutex
	fetched  time.Time
	value    string
	fetching bool
}

func Hostname() string {
	h, _ := os.Hostname()
	return h
}

func OS() string {
	if runtime.GOOS == "windows" {
		return windowsOSDisplay()
	}
	if runtime.GOOS != "linux" {
		return runtime.GOOS
	}
	info := osReleaseInfo()
	for _, key := range []string{"PRETTY_NAME", "NAME", "ID"} {
		if v := strings.TrimSpace(info[key]); v != "" {
			return v
		}
	}
	return "Linux"
}

func Arch() string { return runtime.GOARCH }

func Inventory() map[string]string {
	out := map[string]string{}
	if runtime.GOOS == "windows" {
		return windowsInventory()
	}
	if runtime.GOOS != "linux" {
		out["device_type"] = "desktop"
		return out
	}

	osInfo := osReleaseInfo()
	if v := firstNonEmpty(osInfo["PRETTY_NAME"], osInfo["NAME"]); v != "" {
		out["os_pretty"] = v
	}
	if v := strings.ToLower(strings.TrimSpace(osInfo["ID"])); v != "" {
		out["os_id"] = v
	}
	if v := firstNonEmpty(osInfo["VERSION_ID"], osInfo["VERSION"]); v != "" {
		out["os_version"] = v
	}
	if v := kernelVersion(); v != "" {
		out["kernel_version"] = v
	}
	if v := cpuModel(); v != "" {
		out["cpu_model"] = v
	}
	if cores := cpuCores(); cores > 0 {
		out["cpu_cores"] = strconv.Itoa(cores)
	}

	memTotalKB, memAvailKB := memTotalsKB()
	if memTotalKB > 0 {
		out["memory_total_gb"] = fmt.Sprintf("%.1f", kbToGB(memTotalKB))
		usedKB := memTotalKB - memAvailKB
		if usedKB < 0 {
			usedKB = 0
		}
		out["memory_used_gb"] = fmt.Sprintf("%.1f", kbToGB(usedKB))
	}

	rootTotal, rootFree := diskTotals("/")
	if rootTotal > 0 {
		out["storage_root_total_gb"] = fmt.Sprintf("%.1f", bytesToGB(rootTotal))
	}
	if rootFree >= 0 {
		out["storage_root_free_gb"] = fmt.Sprintf("%.1f", bytesToGB(rootFree))
	}

	if v := readTrimmedFile("/sys/devices/virtual/dmi/id/sys_vendor"); v != "" {
		out["system_vendor"] = v
	}
	if v := readTrimmedFile("/sys/devices/virtual/dmi/id/product_name"); v != "" {
		out["system_product"] = v
	}
	if v := readTrimmedFile("/sys/devices/virtual/dmi/id/product_serial"); v != "" {
		out["system_serial"] = v
	}
	if v := readTrimmedFile("/sys/devices/virtual/dmi/id/bios_version"); v != "" {
		out["bios_version"] = v
	}
	boardVendor := readTrimmedFile("/sys/devices/virtual/dmi/id/board_vendor")
	boardName := readTrimmedFile("/sys/devices/virtual/dmi/id/board_name")
	out["motherboard"] = strings.TrimSpace(strings.Join([]string{boardVendor, boardName}, " "))
	if out["motherboard"] == "" {
		delete(out, "motherboard")
	}
	if chassis := hostnamectlChassis(); chassis != "" {
		out["chassis_type"] = chassis
	}
	if deviceType := detectDeviceType(osInfo); deviceType != "" {
		out["device_type"] = deviceType
	}
	if user := loggedInUser(); user != "" {
		out["logged_in_user"] = user
	}
	if externalIP := publicIPSnapshot(); externalIP != "" {
		out["public_ip"] = externalIP
	}
	if softwareJSON, softwareCount := installedSoftwareSnapshot(); softwareJSON != "" && softwareCount > 0 {
		out["installed_software_json"] = softwareJSON
		out["installed_software_count"] = strconv.Itoa(softwareCount)
	}
	if ips := IPs(); len(ips) > 0 {
		out["local_ip"] = ips[0]
		out["internal_ip"] = ips[0]
		out["local_ips"] = strings.Join(ips, ",")
	}

	return out
}

func IPs() []string {
	if runtime.GOOS == "windows" {
		return prioritizeIP(interfaceIPv4s(), windowsPrimaryIPv4())
	}
	preferred := routeSourceIPv4()
	out := mergeIPv4Lists(
		parseIPsFromIPAddrOutput(shellOutput("ip -o -4 addr show scope global 2>/dev/null || true")),
		parseIPsFromWhitespace(shellOutput("hostname -I 2>/dev/null || true")),
		interfaceIPv4s(),
	)
	if preferred != "" {
		out = prioritizeIP(out, preferred)
	}
	return out
}

func Metrics() map[string]float64 {
	if runtime.GOOS == "windows" {
		return windowsMetrics()
	}
	m := map[string]float64{}
	m["uptime"] = uptimeSeconds()
	m["cpu_usage"] = cpuUsage()
	memTotalKB, memAvailKB := memTotalsKB()
	if memTotalKB > 0 {
		memUsedKB := memTotalKB - memAvailKB
		if memUsedKB < 0 {
			memUsedKB = 0
		}
		m["ram_usage"] = float64(memUsedKB) * 100 / float64(memTotalKB)
		m["ram_total_gb"] = kbToGB(memTotalKB)
		m["ram_used_gb"] = kbToGB(memUsedKB)
	} else {
		m["ram_usage"] = ramUsage()
	}

	rootTotal, rootFree := diskTotals("/")
	if rootTotal > 0 {
		rootUsed := rootTotal - rootFree
		m["disk_usage"] = float64(rootUsed) * 100 / float64(rootTotal)
		m["disk_total_gb"] = bytesToGB(rootTotal)
		m["disk_free_gb"] = bytesToGB(rootFree)
	} else {
		m["disk_usage"] = diskUsage("/")
	}

	m["load_1m"] = loadAverage()
	m["package_updates"] = packageUpdates()
	m["top_proc_cpu"] = topProcessCPU()
	m["cpu_cores"] = float64(cpuCores())
	return m
}

func uptimeSeconds() float64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(b))
	if len(parts) == 0 {
		return 0
	}
	f, _ := strconv.ParseFloat(parts[0], 64)
	return f
}

func ramUsage() float64 {
	total, avail := memTotalsKB()
	if total == 0 {
		return 0
	}
	used := float64(total - avail)
	return used * 100 / float64(total)
}

func diskUsage(path string) float64 {
	return getDiskUsagePercent(path)
}

func cpuUsage() float64 {
	u1, n1, s1, i1 := readCPU()
	time.Sleep(300 * time.Millisecond)
	u2, n2, s2, i2 := readCPU()
	idle := i2 - i1
	total := (u2 - u1) + (n2 - n1) + (s2 - s1) + idle
	if total == 0 {
		return 0
	}
	return float64(total-idle) * 100 / float64(total)
}

func loadAverage() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(b))
	if len(parts) < 1 {
		return 0
	}
	v, _ := strconv.ParseFloat(parts[0], 64)
	return v
}

func packageUpdates() float64 {
	if _, err := os.Stat("/usr/bin/apt"); err == nil {
		cmd := exec.Command("bash", "-lc", "apt list --upgradable 2>/dev/null | sed -n '2,$p' | wc -l")
		b, err := cmd.Output()
		if err == nil {
			n, _ := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
			return n
		}
	}
	if _, err := os.Stat("/usr/bin/dnf"); err == nil {
		cmd := exec.Command("bash", "-lc", "dnf -q check-update 2>/dev/null | grep -E '^[a-zA-Z0-9_.-]+' | wc -l")
		b, err := cmd.Output()
		if err == nil {
			n, _ := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
			return n
		}
	}
	return 0
}

func topProcessCPU() float64 {
	cmd := exec.Command("bash", "-lc", "ps -eo pcpu --no-headers | sort -nr | head -n1")
	b, err := cmd.Output()
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	return v
}

func readCPU() (uint64, uint64, uint64, uint64) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, 0, 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if s.Scan() {
		parts := strings.Fields(s.Text())
		if len(parts) >= 5 {
			u, _ := strconv.ParseUint(parts[1], 10, 64)
			n, _ := strconv.ParseUint(parts[2], 10, 64)
			sy, _ := strconv.ParseUint(parts[3], 10, 64)
			i, _ := strconv.ParseUint(parts[4], 10, 64)
			return u, n, sy, i
		}
	}
	return 0, 0, 0, 0
}

func osReleaseInfo() map[string]string {
	if runtime.GOOS != "linux" {
		return map[string]string{}
	}
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return map[string]string{}
	}
	values := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		values[key] = val
	}
	return values
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if v := strings.TrimSpace(value); v != "" {
			return v
		}
	}
	return ""
}

func memTotalsKB() (int64, int64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var totalKB int64
	var availKB int64
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				totalKB, _ = strconv.ParseInt(parts[1], 10, 64)
			}
		} else if strings.HasPrefix(line, "MemAvailable:") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				availKB, _ = strconv.ParseInt(parts[1], 10, 64)
			}
		}
	}
	if availKB == 0 && totalKB > 0 {
		availKB = totalKB
	}
	return totalKB, availKB
}

func diskTotals(path string) (int64, int64) {
	return getDiskTotals(path)
}

func kbToGB(kb int64) float64 {
	return float64(kb) / (1024.0 * 1024.0)
}

func bytesToGB(v int64) float64 {
	return float64(v) / (1024.0 * 1024.0 * 1024.0)
}

func kernelVersion() string {
	if v := readTrimmedFile("/proc/sys/kernel/osrelease"); v != "" {
		return v
	}
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func cpuModel() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(strings.ToLower(line), "model name") || strings.HasPrefix(strings.ToLower(line), "hardware") {
			_, rhs, ok := strings.Cut(line, ":")
			if ok {
				return strings.TrimSpace(rhs)
			}
		}
	}
	return ""
}

func cpuCores() int {
	f, err := os.Open("/proc/cpuinfo")
	if err == nil {
		defer f.Close()
		count := 0
		s := bufio.NewScanner(f)
		for s.Scan() {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(s.Text())), "processor") {
				count++
			}
		}
		if count > 0 {
			return count
		}
	}
	return runtime.NumCPU()
}

func readTrimmedFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func hostnamectlChassis() string {
	out, err := exec.Command("hostnamectl", "chassis").Output()
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(out)))
}

func chassisTypeCode() int {
	raw := readTrimmedFile("/sys/devices/virtual/dmi/id/chassis_type")
	if raw == "" {
		raw = readTrimmedFile("/sys/class/dmi/id/chassis_type")
	}
	if raw == "" {
		return 0
	}
	n, _ := strconv.Atoi(raw)
	return n
}

func defaultTarget() string {
	target := readTrimmedFile("/etc/systemd/system/default.target")
	if strings.HasSuffix(target, ".target") {
		return target
	}
	link, err := os.Readlink("/etc/systemd/system/default.target")
	if err != nil {
		return ""
	}
	base := filepath.Base(link)
	if strings.HasSuffix(base, ".target") {
		return strings.ToLower(base)
	}
	return strings.ToLower(strings.TrimSpace(link))
}

func hasBattery() bool {
	matches, _ := filepath.Glob("/sys/class/power_supply/BAT*")
	return len(matches) > 0
}

func isRaspberryPi() bool {
	model := strings.ToLower(readTrimmedFile("/proc/device-tree/model"))
	if strings.Contains(model, "raspberry") {
		return true
	}
	cpuInfo := strings.ToLower(readTrimmedFile("/proc/cpuinfo"))
	return strings.Contains(cpuInfo, "raspberry pi")
}

func detectDeviceType(osInfo map[string]string) string {
	if runtime.GOOS != "linux" {
		return "desktop"
	}
	if isRaspberryPi() {
		return "rpi"
	}
	if chassis := hostnamectlChassis(); chassis != "" {
		switch {
		case strings.Contains(chassis, "server"), strings.Contains(chassis, "vm"), strings.Contains(chassis, "virtual"), strings.Contains(chassis, "container"):
			return "server"
		case strings.Contains(chassis, "desktop"), strings.Contains(chassis, "laptop"), strings.Contains(chassis, "notebook"), strings.Contains(chassis, "tablet"), strings.Contains(chassis, "handset"):
			return "desktop"
		}
	}
	switch chassisTypeCode() {
	case 17, 22, 23, 28, 29, 33:
		return "server"
	case 3, 4, 5, 6, 7, 13, 15, 24, 25, 35, 36, 8, 9, 10, 14, 31, 32:
		return "desktop"
	}
	if hasBattery() {
		return "desktop"
	}
	target := defaultTarget()
	if strings.Contains(target, "graphical.target") {
		return "desktop"
	}
	if strings.Contains(target, "multi-user.target") {
		return "server"
	}
	osName := strings.ToLower(firstNonEmpty(osInfo["PRETTY_NAME"], osInfo["NAME"]))
	if strings.Contains(osName, "server") {
		return "server"
	}
	return "server"
}

func installedSoftwareSnapshot() (string, int) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return "", 0
	}
	now := time.Now()
	softwareInventoryCache.mu.Lock()
	defer softwareInventoryCache.mu.Unlock()

	if softwareInventoryCache.json != "" && now.Sub(softwareInventoryCache.scannedAt) < 15*time.Minute {
		return softwareInventoryCache.json, softwareInventoryCache.count
	}

	entries := collectInstalledSoftware()
	if runtime.GOOS == "windows" {
		entries = collectInstalledSoftwareWindows()
	}
	if len(entries) == 0 {
		softwareInventoryCache.scannedAt = now
		softwareInventoryCache.json = ""
		softwareInventoryCache.count = 0
		return "", 0
	}
	if len(entries) > 300 {
		entries = entries[:300]
	}
	b, err := json.Marshal(entries)
	if err != nil {
		softwareInventoryCache.scannedAt = now
		softwareInventoryCache.json = ""
		softwareInventoryCache.count = 0
		return "", 0
	}
	softwareInventoryCache.scannedAt = now
	softwareInventoryCache.json = string(b)
	softwareInventoryCache.count = len(entries)
	return softwareInventoryCache.json, softwareInventoryCache.count
}

func collectInstalledSoftwareWindows() []softwareEntry {
	raw := strings.TrimSpace(psOutput(`$paths=@('HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*','HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*');
$items=@();
foreach($p in $paths){
  $items += Get-ItemProperty $p -ErrorAction SilentlyContinue |
    Where-Object { $_.DisplayName -and $_.DisplayName.Trim().Length -gt 0 -and -not $_.SystemComponent } |
    Select-Object @{n='name';e={$_.DisplayName}}, @{n='version';e={$_.DisplayVersion}}, @{n='source';e={'windows-registry'}}
}
$items | Sort-Object name, version -Unique | ConvertTo-Json -Compress`))
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "{") {
		raw = "[" + raw + "]"
	}
	parsed := []softwareEntry{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	out := make([]softwareEntry, 0, len(parsed))
	seen := map[string]bool{}
	for _, item := range parsed {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name + "|" + strings.TrimSpace(item.Version))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, softwareEntry{
			Name:    name,
			Version: strings.TrimSpace(item.Version),
			Source:  "windows-registry",
		})
	}
	sort.Slice(out, func(i, j int) bool {
		li := strings.ToLower(out[i].Name)
		lj := strings.ToLower(out[j].Name)
		if li == lj {
			return strings.ToLower(out[i].Version) < strings.ToLower(out[j].Version)
		}
		return li < lj
	})
	return out
}

func collectInstalledSoftware() []softwareEntry {
	out := make([]softwareEntry, 0, 64)
	seen := map[string]bool{}
	add := func(name, version, source string) {
		name = strings.TrimSpace(name)
		version = strings.TrimSpace(version)
		source = strings.TrimSpace(source)
		if name == "" {
			return
		}
		key := strings.ToLower(name + "|" + source)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, softwareEntry{Name: name, Version: version, Source: source})
	}

	collectAPTPackages(add)
	collectDNFPackages(add)
	collectPacmanPackages(add)
	collectSnapApps(add)
	collectFlatpakApps(add)

	sort.Slice(out, func(i, j int) bool {
		li := strings.ToLower(out[i].Name)
		lj := strings.ToLower(out[j].Name)
		if li == lj {
			return strings.ToLower(out[i].Source) < strings.ToLower(out[j].Source)
		}
		return li < lj
	})
	return out
}

func loggedInUser() string {
	if runtime.GOOS == "windows" {
		user := strings.TrimSpace(psOutput(`$u=(Get-CimInstance Win32_ComputerSystem -ErrorAction SilentlyContinue).UserName; if($u){$u} else {$env:USERNAME}`))
		user = strings.TrimSpace(strings.TrimPrefix(user, ".\\"))
		return user
	}
	candidates := []string{
		"who 2>/dev/null | awk 'NF {print $1; exit}'",
		"loginctl list-users --no-legend 2>/dev/null | awk 'NF {print $2; exit}'",
		"users 2>/dev/null | awk 'NF {print $1; exit}'",
	}
	for _, cmdText := range candidates {
		out, err := exec.Command("bash", "-lc", cmdText).Output()
		if err != nil {
			continue
		}
		value := strings.TrimSpace(string(out))
		if value == "" {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		user := strings.TrimSpace(fields[0])
		if user != "" {
			return user
		}
	}
	return ""
}

func publicIPSnapshot() string {
	now := time.Now()
	publicIPCache.mu.Lock()
	if publicIPCache.value != "" && now.Sub(publicIPCache.fetched) < 20*time.Minute {
		value := publicIPCache.value
		publicIPCache.mu.Unlock()
		return value
	}
	publicIPCache.mu.Unlock()

	client := &http.Client{Timeout: 2500 * time.Millisecond}
	endpoints := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
	}
	for _, endpoint := range endpoints {
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "NerdyAgent/0.3.5")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode >= 300 {
			continue
		}
		value := strings.TrimSpace(string(body))
		if strings.Contains(value, "\n") {
			value = strings.TrimSpace(strings.Split(value, "\n")[0])
		}
		if ip := net.ParseIP(value); ip == nil {
			continue
		}
		publicIPCache.mu.Lock()
		publicIPCache.value = value
		publicIPCache.fetched = now
		publicIPCache.mu.Unlock()
		return value
	}

	publicIPCache.mu.Lock()
	fallback := publicIPCache.value
	publicIPCache.mu.Unlock()
	return fallback
}

func collectAPTPackages(add func(name, version, source string)) {
	if !commandExists("apt-mark") || !commandExists("dpkg-query") {
		return
	}
	manualOut, err := exec.Command("bash", "-lc", "apt-mark showmanual 2>/dev/null").Output()
	if err != nil {
		return
	}
	for _, rawPkg := range strings.Split(string(manualOut), "\n") {
		pkg := strings.TrimSpace(rawPkg)
		if pkg == "" {
			continue
		}
		metaOut, err := exec.Command("dpkg-query", "-W", "-f=${Package}\t${Version}\t${Priority}\t${Section}\n", pkg).Output()
		if err != nil {
			continue
		}
		line := strings.TrimSpace(string(metaOut))
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		name := strings.TrimSpace(parts[0])
		version := ""
		priority := ""
		section := ""
		if len(parts) > 1 {
			version = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			priority = strings.TrimSpace(parts[2])
		}
		if len(parts) > 3 {
			section = strings.TrimSpace(parts[3])
		}
		if isLikelySystemPackage(name, priority, section) {
			continue
		}
		add(name, version, "apt")
	}
}

func collectDNFPackages(add func(name, version, source string)) {
	if !commandExists("dnf") {
		return
	}
	out, err := exec.Command("bash", "-lc", "dnf repoquery --userinstalled --qf '%{name}\t%{version}-%{release}' 2>/dev/null || true").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		name := strings.TrimSpace(parts[0])
		version := ""
		if len(parts) > 1 {
			version = strings.TrimSpace(parts[1])
		}
		if isLikelySystemPackage(name, "", "") {
			continue
		}
		add(name, version, "dnf")
	}
}

func collectPacmanPackages(add func(name, version, source string)) {
	if !commandExists("pacman") {
		return
	}
	out, err := exec.Command("bash", "-lc", "pacman -Qe 2>/dev/null || true").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		version := strings.TrimSpace(parts[1])
		if isLikelySystemPackage(name, "", "") {
			continue
		}
		add(name, version, "pacman")
	}
}

func collectSnapApps(add func(name, version, source string)) {
	if !commandExists("snap") {
		return
	}
	out, err := exec.Command("bash", "-lc", "snap list 2>/dev/null | sed -n '2,$p'").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		add(parts[0], parts[1], "snap")
	}
}

func collectFlatpakApps(add func(name, version, source string)) {
	if !commandExists("flatpak") {
		return
	}
	out, err := exec.Command("bash", "-lc", "flatpak list --app --columns=application,version 2>/dev/null || true").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		name := strings.TrimSpace(parts[0])
		version := ""
		if len(parts) > 1 {
			version = strings.TrimSpace(parts[1])
		}
		add(name, version, "flatpak")
	}
}

func commandExists(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

func isLikelySystemPackage(name, priority, section string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	p := strings.ToLower(strings.TrimSpace(priority))
	s := strings.ToLower(strings.TrimSpace(section))
	if n == "" {
		return true
	}
	if p == "required" || p == "important" || p == "essential" || p == "standard" {
		return true
	}
	if strings.HasPrefix(n, "lib") ||
		strings.HasPrefix(n, "linux-") ||
		strings.HasPrefix(n, "gir1.") ||
		strings.HasPrefix(n, "python3-") ||
		strings.HasPrefix(n, "perl-") {
		return true
	}
	if s == "admin" || s == "libs" || s == "kernel" || s == "debug" || s == "devel" || s == "doc" || s == "localization" {
		return true
	}
	return false
}

func shellOutput(command string) string {
	out, err := exec.Command("bash", "-lc", command).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func parseIPsFromWhitespace(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	for _, field := range strings.Fields(raw) {
		ip := net.ParseIP(strings.TrimSpace(field))
		if ip == nil || ip.IsLoopback() {
			continue
		}
		v4 := ip.To4()
		if v4 == nil {
			continue
		}
		value := v4.String()
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func parseIPsFromIPAddrOutput(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		for i := 0; i < len(parts); i++ {
			if parts[i] != "inet" || i+1 >= len(parts) {
				continue
			}
			cidr := strings.TrimSpace(parts[i+1])
			ipRaw := cidr
			if slash := strings.Index(cidr, "/"); slash > 0 {
				ipRaw = cidr[:slash]
			}
			ip := net.ParseIP(ipRaw)
			if ip == nil || ip.IsLoopback() {
				continue
			}
			v4 := ip.To4()
			if v4 == nil {
				continue
			}
			value := v4.String()
			if seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func interfaceIPv4s() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil || ip == nil {
				continue
			}
			v4 := ip.To4()
			if v4 == nil || v4.IsLoopback() {
				continue
			}
			value := v4.String()
			if seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func routeSourceIPv4() string {
	out := strings.TrimSpace(shellOutput("ip -4 route get 1.1.1.1 2>/dev/null || true"))
	if out == "" {
		return ""
	}
	fields := strings.Fields(out)
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] != "src" {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(fields[i+1]))
		if ip == nil || ip.IsLoopback() {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}

func mergeIPv4Lists(lists ...[]string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 6)
	for _, list := range lists {
		for _, candidate := range list {
			ip := net.ParseIP(strings.TrimSpace(candidate))
			if ip == nil || ip.IsLoopback() {
				continue
			}
			v4 := ip.To4()
			if v4 == nil {
				continue
			}
			value := v4.String()
			if seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func prioritizeIP(ips []string, preferred string) []string {
	preferred = strings.TrimSpace(preferred)
	if preferred == "" || len(ips) == 0 {
		return ips
	}
	index := -1
	for i, ip := range ips {
		if strings.TrimSpace(ip) == preferred {
			index = i
			break
		}
	}
	if index <= 0 {
		if index < 0 {
			return append([]string{preferred}, ips...)
		}
		return ips
	}
	out := make([]string, 0, len(ips))
	out = append(out, preferred)
	for i, ip := range ips {
		if i == index {
			continue
		}
		out = append(out, ip)
	}
	return out
}

func windowsOSDisplay() string {
	caption := strings.TrimSpace(psOutput(`(Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue).Caption`))
	version := strings.TrimSpace(psOutput(`(Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue).Version`))
	build := strings.TrimSpace(psOutput(`(Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue).BuildNumber`))
	if caption == "" {
		caption = "Windows"
	}
	parts := []string{caption}
	if version != "" {
		parts = append(parts, version)
	}
	if build != "" {
		parts = append(parts, "build "+build)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func windowsInventory() map[string]string {
	out := map[string]string{}
	osDisplay := windowsOSDisplay()
	out["os_pretty"] = osDisplay
	out["os_id"] = "windows"
	out["os_version"] = strings.TrimSpace(psOutput(`(Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue).Version`))
	out["kernel_version"] = strings.TrimSpace(psOutput(`(Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue).BuildNumber`))
	out["cpu_model"] = strings.TrimSpace(psOutput(`(Get-CimInstance Win32_Processor -ErrorAction SilentlyContinue | Select-Object -First 1).Name`))
	out["system_vendor"] = strings.TrimSpace(psOutput(`(Get-CimInstance Win32_ComputerSystem -ErrorAction SilentlyContinue).Manufacturer`))
	out["system_product"] = strings.TrimSpace(psOutput(`(Get-CimInstance Win32_ComputerSystem -ErrorAction SilentlyContinue).Model`))
	out["bios_version"] = strings.TrimSpace(psOutput(`(Get-CimInstance Win32_BIOS -ErrorAction SilentlyContinue | Select-Object -First 1).SMBIOSBIOSVersion`))
	out["system_serial"] = strings.TrimSpace(psOutput(`(Get-CimInstance Win32_BIOS -ErrorAction SilentlyContinue | Select-Object -First 1).SerialNumber`))
	out["motherboard"] = strings.TrimSpace(psOutput(`(Get-CimInstance Win32_BaseBoard -ErrorAction SilentlyContinue | Select-Object -First 1).Product`))
	out["logged_in_user"] = strings.TrimSpace(psOutput(`$u=(Get-CimInstance Win32_ComputerSystem -ErrorAction SilentlyContinue).UserName; if($u){$u} else {$env:USERNAME}`))
	out["domain"] = strings.TrimSpace(psOutput(`$cs=Get-CimInstance Win32_ComputerSystem -ErrorAction SilentlyContinue; if($cs){if($cs.PartOfDomain){$cs.Domain}else{$cs.Workgroup}}`))
	out["workgroup"] = strings.TrimSpace(psOutput(`$cs=Get-CimInstance Win32_ComputerSystem -ErrorAction SilentlyContinue; if($cs -and -not $cs.PartOfDomain){$cs.Workgroup}`))
	out["av_status"] = strings.TrimSpace(psOutput(`$names=(Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntiVirusProduct -ErrorAction SilentlyContinue | Select-Object -ExpandProperty displayName); if($names){($names -join ', ')} else {$mp=Get-MpComputerStatus -ErrorAction SilentlyContinue; if($mp){if($mp.AntivirusEnabled){'Microsoft Defender (Enabled)'} else {'Microsoft Defender (Disabled)'}}}`))
	out["bitlocker_status"] = strings.TrimSpace(psOutput(`$b=Get-BitLockerVolume -MountPoint 'C:' -ErrorAction SilentlyContinue | Select-Object -First 1; if($b){if($b.ProtectionStatus -eq 1){'On'} elseif($b.ProtectionStatus -eq 0){'Off'} else {'Unknown'}}`))
	productType := strings.TrimSpace(psOutput(`(Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue).ProductType`))
	if productType == "1" {
		out["device_type"] = "desktop"
	} else if productType != "" {
		out["device_type"] = "server"
	} else {
		out["device_type"] = "desktop"
	}
	if ips := IPs(); len(ips) > 0 {
		out["local_ip"] = ips[0]
		out["internal_ip"] = ips[0]
		out["local_ips"] = strings.Join(ips, ",")
	}
	if ext := publicIPSnapshot(); ext != "" {
		out["public_ip"] = ext
	}
	memTotalRaw := strings.TrimSpace(psOutput(`[int64](Get-CimInstance Win32_ComputerSystem -ErrorAction SilentlyContinue).TotalPhysicalMemory`))
	memAvailRaw := strings.TrimSpace(psOutput(`[int64](Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue).FreePhysicalMemory * 1024`))
	memTotal, _ := strconv.ParseInt(memTotalRaw, 10, 64)
	memAvail, _ := strconv.ParseInt(memAvailRaw, 10, 64)
	if memTotal > 0 {
		out["memory_total_gb"] = fmt.Sprintf("%.1f", bytesToGB(memTotal))
		used := memTotal - memAvail
		if used < 0 {
			used = 0
		}
		out["memory_used_gb"] = fmt.Sprintf("%.1f", bytesToGB(used))
	}
	rootTotal, rootFree := getDiskTotals("C:")
	if rootTotal > 0 {
		out["storage_root_total_gb"] = fmt.Sprintf("%.1f", bytesToGB(rootTotal))
	}
	if rootFree >= 0 {
		out["storage_root_free_gb"] = fmt.Sprintf("%.1f", bytesToGB(rootFree))
	}
	return out
}

func windowsMetrics() map[string]float64 {
	m := map[string]float64{}
	m["uptime"] = windowsUptimeSeconds()
	m["cpu_usage"] = windowsCPUUsage()
	memTotalRaw := strings.TrimSpace(psOutput(`[int64](Get-CimInstance Win32_ComputerSystem -ErrorAction SilentlyContinue).TotalPhysicalMemory`))
	memAvailRaw := strings.TrimSpace(psOutput(`[int64](Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue).FreePhysicalMemory * 1024`))
	memTotal, _ := strconv.ParseInt(memTotalRaw, 10, 64)
	memAvail, _ := strconv.ParseInt(memAvailRaw, 10, 64)
	if memTotal > 0 {
		used := memTotal - memAvail
		if used < 0 {
			used = 0
		}
		m["ram_usage"] = float64(used) * 100 / float64(memTotal)
		m["ram_total_gb"] = bytesToGB(memTotal)
		m["ram_used_gb"] = bytesToGB(used)
	}
	rootTotal, rootFree := getDiskTotals("C:")
	if rootTotal > 0 {
		rootUsed := rootTotal - rootFree
		m["disk_usage"] = float64(rootUsed) * 100 / float64(rootTotal)
		m["disk_total_gb"] = bytesToGB(rootTotal)
		m["disk_free_gb"] = bytesToGB(rootFree)
	}
	m["load_1m"] = 0
	m["package_updates"] = 0
	m["top_proc_cpu"] = 0
	m["cpu_cores"] = float64(runtime.NumCPU())
	return m
}

func windowsUptimeSeconds() float64 {
	raw := strings.TrimSpace(psOutput(`$last=(Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue).LastBootUpTime; if($last){[int]((Get-Date)-$last).TotalSeconds}else{0}`))
	v, _ := strconv.ParseFloat(raw, 64)
	return v
}

func windowsCPUUsage() float64 {
	raw := strings.TrimSpace(psOutput(`$v=(Get-Counter '\Processor(_Total)\%% Processor Time' -ErrorAction SilentlyContinue).CounterSamples | Select-Object -First 1; if($v){[math]::Round($v.CookedValue,2)} else {0}`))
	v, _ := strconv.ParseFloat(raw, 64)
	return v
}

func windowsPrimaryIPv4() string {
	raw := strings.TrimSpace(psOutput(`$r=Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Sort-Object RouteMetric | Select-Object -First 1; if($r){$i=Get-NetIPAddress -AddressFamily IPv4 -InterfaceIndex $r.InterfaceIndex -ErrorAction SilentlyContinue | Where-Object {$_.IPAddress -notlike '169.254*'} | Select-Object -First 1; if($i){$i.IPAddress}}`))
	ip := net.ParseIP(raw)
	if ip == nil || ip.To4() == nil {
		return ""
	}
	return ip.To4().String()
}

func psOutput(command string) string {
	if runtime.GOOS != "windows" {
		return ""
	}
	out, err := exec.Command("powershell", "-NoProfile", "-Command", command).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
