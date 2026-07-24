//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeRegistrationEmailSuffixBlacklist(t *testing.T) {
	got, err := NormalizeRegistrationEmailSuffixBlacklist([]string{"example.com", "@EXAMPLE.COM", " @foo.bar ", "*.EDU.CN"})
	require.NoError(t, err)
	require.Equal(t, []string{"@example.com", "@foo.bar", "*.edu.cn"}, got)
}

func TestNormalizeRegistrationEmailSuffixBlacklist_Invalid(t *testing.T) {
	for _, item := range []string{"@invalid_domain", "*.", "*", "*.@", "*.foo"} {
		t.Run(item, func(t *testing.T) {
			_, err := NormalizeRegistrationEmailSuffixBlacklist([]string{item})
			require.Error(t, err)
		})
	}
}

func TestParseRegistrationEmailSuffixBlacklist(t *testing.T) {
	got := ParseRegistrationEmailSuffixBlacklist(`["example.com","@foo.bar","*.EDU.CN","@invalid_domain","*.foo"]`)
	require.Equal(t, []string{"@example.com", "@foo.bar", "*.edu.cn"}, got)
}

func TestIsRegistrationEmailSuffixBlocked(t *testing.T) {
	require.True(t, IsRegistrationEmailSuffixBlocked("user@example.com", []string{"@example.com"}))
	require.False(t, IsRegistrationEmailSuffixBlocked("user@sub.example.com", []string{"@example.com"}))
	require.True(t, IsRegistrationEmailSuffixBlocked("user@qq.com", []string{"@qq.com"}))
	require.False(t, IsRegistrationEmailSuffixBlocked("user@sub.qq.com", []string{"@qq.com"}))
	require.True(t, IsRegistrationEmailSuffixBlocked("student@cs.edu.cn", []string{"*.edu.cn"}))
	require.True(t, IsRegistrationEmailSuffixBlocked("student@edu.cn", []string{"*.edu.cn"}))
	require.False(t, IsRegistrationEmailSuffixBlocked("student@foo.cn", []string{"*.edu.cn"}))
	require.True(t, IsRegistrationEmailSuffixBlocked("user@a.com", []string{"@a.com", "*.b.cn"}))
	require.True(t, IsRegistrationEmailSuffixBlocked("user@school.b.cn", []string{"@a.com", "*.b.cn"}))
	require.True(t, IsRegistrationEmailSuffixBlocked("user@b.cn", []string{"@a.com", "*.b.cn"}))
	require.False(t, IsRegistrationEmailSuffixBlocked("user@c.cn", []string{"@a.com", "*.b.cn"}))
	require.False(t, IsRegistrationEmailSuffixBlocked("user@any.com", []string{}))
}
