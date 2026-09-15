package updater

import (
	"strings"
)

// CompareVersions returns -1 if current < target, 0 if equal, 1 if current > target.
func CompareVersions(current, target string) int {
	a := parseVersionParts(current)
	b := parseVersionParts(target)
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		av := 0
		if i < len(a) {
			av = a[i]
		}
		bv := 0
		if i < len(b) {
			bv = b[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func parseVersionParts(v string) []int {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(v), "v"))
	if trimmed == "" {
		return []int{0}
	}
	parts := strings.Split(trimmed, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		digits := make([]rune, 0, len(part))
		for _, r := range part {
			if r >= '0' && r <= '9' {
				digits = append(digits, r)
			} else {
				break
			}
		}
		if len(digits) == 0 {
			out = append(out, 0)
			continue
		}
		n := 0
		for _, r := range digits {
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}
