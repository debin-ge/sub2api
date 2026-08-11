package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/internalrelay"
)

func TestParseDebugEnvBool(t *testing.T) {
	t.Run("empty is false", func(t *testing.T) {
		if parseDebugEnvBool("") {
			t.Fatalf("expected false for empty string")
		}
	})

	t.Run("true-like values", func(t *testing.T) {
		for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
			t.Run(value, func(t *testing.T) {
				if !parseDebugEnvBool(value) {
					t.Fatalf("expected true for %q", value)
				}
			})
		}
	})

	t.Run("false-like values", func(t *testing.T) {
		for _, value := range []string{"0", "false", "off", "debug"} {
			t.Run(value, func(t *testing.T) {
				if parseDebugEnvBool(value) {
					t.Fatalf("expected false for %q", value)
				}
			})
		}
	})
}

func TestSafeHeaderValueForLogRedactsInternalRelayMarker(t *testing.T) {
	const marker = "signed.internal.relay.marker"
	if got := safeHeaderValueForLog(internalrelay.HeaderName, marker); got != "[redacted]" {
		t.Fatalf("internal relay marker was not redacted: %q", got)
	}
}
