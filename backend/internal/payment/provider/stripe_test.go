//go:build unit

package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveStripeMethodTypesMapsGooglePayToDeduplicatedCard(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"card"}, resolveStripeMethodTypes("card,google_pay"))
	require.Equal(t, []string{"card", "link"}, resolveStripeMethodTypes("google_pay,card,link"))
	require.Equal(t, []string{"link", "card"}, resolveStripeMethodTypes("link,google_pay"))
}
