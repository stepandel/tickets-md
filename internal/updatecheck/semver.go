package updatecheck

import (
	"strconv"
	"strings"
)

type semver struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func compareVersions(current, latest string) (int, bool) {
	a, ok := parseSemver(current)
	if !ok {
		return 0, false
	}
	b, ok := parseSemver(latest)
	if !ok {
		return 0, false
	}
	return compareSemver(a, b), true
}

func parseSemver(s string) (semver, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "dev" || s == "(devel)" {
		return semver{}, false
	}
	s = strings.TrimPrefix(s, "v")
	base := s
	prerelease := ""
	if i := strings.IndexByte(s, '-'); i >= 0 {
		base = s[:i]
		prerelease = s[i+1:]
		if prerelease == "" {
			return semver{}, false
		}
	}
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semver{}, false
	}
	return semver{
		major:      major,
		minor:      minor,
		patch:      patch,
		prerelease: prerelease,
	}, true
}

func compareSemver(a, b semver) int {
	if a.major != b.major {
		if a.major < b.major {
			return -1
		}
		return 1
	}
	if a.minor != b.minor {
		if a.minor < b.minor {
			return -1
		}
		return 1
	}
	if a.patch != b.patch {
		if a.patch < b.patch {
			return -1
		}
		return 1
	}
	switch {
	case a.prerelease == "" && b.prerelease == "":
		return 0
	case a.prerelease == "":
		return 1
	case b.prerelease == "":
		return -1
	default:
		return strings.Compare(a.prerelease, b.prerelease)
	}
}
