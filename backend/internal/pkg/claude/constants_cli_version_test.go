package claude

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const fable51MinCLIVersion = "2.1.251"

func TestCLICurrentVersionMatchesDefaultUserAgent(t *testing.T) {
	t.Parallel()

	ua := DefaultHeaders["User-Agent"]
	const prefix = "claude-cli/"
	require.True(t, strings.HasPrefix(ua, prefix), "unexpected User-Agent: %q", ua)

	rest := strings.TrimPrefix(ua, prefix)
	version := rest
	if idx := strings.IndexByte(rest, ' '); idx >= 0 {
		version = rest[:idx]
	}
	require.Equal(t, CLICurrentVersion, version)
}

func TestCLICurrentVersionSatisfiesFable51Gate(t *testing.T) {
	t.Parallel()

	require.GreaterOrEqual(t, compareThreePartSemver(t, CLICurrentVersion, fable51MinCLIVersion), 0,
		"CLICurrentVersion %s is below the claude-fable-5-1 gate %s",
		CLICurrentVersion, fable51MinCLIVersion)
}

func compareThreePartSemver(t *testing.T, a, b string) int {
	t.Helper()

	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	require.Len(t, as, 3, "not a three-part semver: %q", a)
	require.Len(t, bs, 3, "not a three-part semver: %q", b)

	for i := range as {
		ai, err := strconv.Atoi(as[i])
		require.NoError(t, err, "non-numeric segment in %q", a)
		bi, err := strconv.Atoi(bs[i])
		require.NoError(t, err, "non-numeric segment in %q", b)

		switch {
		case ai > bi:
			return 1
		case ai < bi:
			return -1
		}
	}
	return 0
}
