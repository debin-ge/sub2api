package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/internalrelay"
)

// applyInternalRelayHeader is the final outbound header guard for OpenAI
// traffic. It always removes any caller/account supplied value first, then
// adds a short-lived authenticated marker only for an eligible loopback
// account with a request-scoped client correlation ID.
func (s *OpenAIGatewayService) applyInternalRelayHeader(ctx context.Context, account *Account, headers http.Header) {
	jwtSecret := ""
	if s != nil && s.cfg != nil {
		jwtSecret = s.cfg.JWT.Secret
	}
	applyInternalRelayHeaderWithSecret(ctx, jwtSecret, account, headers)
}

func applyInternalRelayHeaderWithSecret(ctx context.Context, jwtSecret string, account *Account, headers http.Header) {
	if headers == nil {
		return
	}
	headers.Del(internalrelay.HeaderName)
	if account == nil || !account.IsInternalRelay() || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return
	}
	if !internalrelay.IsLoopbackBaseURL(account.GetCredential("base_url")) {
		return
	}
	if ctx == nil {
		return
	}
	clientRequestID, _ := ctx.Value(ctxkey.ClientRequestID).(string)
	parentRequestID := internalrelay.ClientRequestID(clientRequestID)
	if strings.TrimSpace(parentRequestID) == "" {
		return
	}
	marker, err := internalrelay.NewSigner(jwtSecret).Sign(account.ID, parentRequestID, time.Now())
	if err != nil {
		return
	}
	headers.Set(internalrelay.HeaderName, marker)
}
