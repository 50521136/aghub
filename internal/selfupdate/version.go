package selfupdate

import (
	"strconv"
	"strings"
)

// compareVersions compares two semantic versions, ignoring any pre-release and
// build metadata.  It returns a negative number if a is older than b, zero if
// they are equal, and a positive number otherwise.
func compareVersions(a, b string) (cmp int) {
	an := parseVersion(a)
	bn := parseVersion(b)

	for i := range 3 {
		if an[i] != bn[i] {
			return an[i] - bn[i]
		}
	}

	return 0
}

// parseVersion parses a version string into three numeric components.  Missing
// or non-numeric components are treated as zero.
func parseVersion(v string) (out [3]int) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")

	// Drop the pre-release and build metadata.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}

	for i, part := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}

		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			n = 0
		}

		out[i] = n
	}

	return out
}

// formatVersion returns a human-readable version string.
func formatVersion(v string) (out string) {
	if v == "" {
		return "unknown"
	}

	if strings.HasPrefix(v, "v") {
		return v
	}

	return "v" + v
}

// describeVersion returns a version string with a human-readable description.
func describeVersion(v string) (out string) {
	if v == "" || v == "dev" {
		return "development build"
	}

	return formatVersion(v)
}
