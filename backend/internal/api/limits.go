package api

import "regexp"

// validCPULimit accepts compose's cpu shorthand: a positive decimal
// number with up to 3 fractional digits. Rejects everything else so
// `docker compose up` doesn't blow up at deploy time with a confusing
// parse error.
var cpuLimitRe = regexp.MustCompile(`^\d+(\.\d{1,3})?$`)

func validCPULimit(s string) bool {
	if s == "0" || s == "0.0" || s == "0.00" || s == "0.000" {
		return false
	}
	return cpuLimitRe.MatchString(s)
}

// validMemoryLimit accepts compose's memory shorthand: <int>[bkmg]
// (case-insensitive). The lowercase set is what compose prefers; we
// normalise nothing — pass through to docker.
var memLimitRe = regexp.MustCompile(`^\d+[bBkKmMgG]?$`)

func validMemoryLimit(s string) bool {
	if s == "0" || s == "0b" || s == "0k" || s == "0m" || s == "0g" {
		return false
	}
	return memLimitRe.MatchString(s)
}
