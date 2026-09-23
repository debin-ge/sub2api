//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageTokensFromClaudeUsageKeepsCacheDimensions(t *testing.T) {
	tokens := usageTokensFromClaudeUsage(ClaudeUsage{
		InputTokens:              100,
		OutputTokens:             20,
		CacheCreationInputTokens: 30,
		CacheReadInputTokens:     400,
		CacheCreation5mTokens:    10,
		CacheCreation1hTokens:    20,
		ImageOutputTokens:        1290,
	})
	require.Equal(t, UsageTokens{
		InputTokens:           100,
		OutputTokens:          20,
		CacheCreationTokens:   30,
		CacheReadTokens:       400,
		CacheCreation5mTokens: 10,
		CacheCreation1hTokens: 20,
		ImageOutputTokens:     1290,
	}, tokens)
}
