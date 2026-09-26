//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// generateRandomString 是 out_trade_no 的随机后缀来源，必须：长度精确、只落在字符集内、
// 连续两次不相同（crypto/rand 拒绝采样实现，见 payment_service.go）。
func TestGenerateRandomString_LengthAndCharset(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 8, 16, 64, 300} {
		got := generateRandomString(n)
		require.Len(t, got, n, "n=%d", n)
		for i, ch := range got {
			require.True(t, strings.ContainsRune(randomStringCharset, ch), "n=%d index=%d char=%q", n, i, ch)
		}
	}
}

func TestGenerateRandomString_NonPositiveLengthIsEmpty(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", generateRandomString(0))
	require.Equal(t, "", generateRandomString(-3))
}

func TestGenerateRandomString_ConsecutiveCallsDiffer(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{}, 64)
	for range 64 {
		got := generateRandomString(8)
		_, dup := seen[got]
		require.False(t, dup, "duplicate 8-char random string %q within 64 draws", got)
		seen[got] = struct{}{}
	}
}

// 粗粒度均匀性检查：62 个字符、大量采样后每个字符都应出现，且没有字符占比离谱
// （拒绝采样若退化为取模会让前 8 个字符偏多约 25%）。
func TestGenerateRandomString_RejectionSamplingCoversWholeCharset(t *testing.T) {
	t.Parallel()
	const draws = 62 * 400
	counts := make(map[byte]int, len(randomStringCharset))
	sample := generateRandomString(draws)
	for i := 0; i < len(sample); i++ {
		counts[sample[i]]++
	}
	require.Len(t, counts, len(randomStringCharset), "every charset byte should appear")
	expected := float64(draws) / float64(len(randomStringCharset))
	for ch, count := range counts {
		ratio := float64(count) / expected
		require.InDelta(t, 1.0, ratio, 0.5, "char %q ratio %.2f is suspicious", ch, ratio)
	}
}
