package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/internalrelay"
)

// applyInternalRelayHeader is the final outbound header guard used by the
// OpenAI-family gateway. Other API-key platform gateways use
// applyInternalRelayHeaderFromContext so they do not need their own config
// dependency.
func (s *OpenAIGatewayService) applyInternalRelayHeader(ctx context.Context, account *Account, headers http.Header) {
	jwtSecret := ""
	if s != nil && s.cfg != nil {
		jwtSecret = s.cfg.JWT.Secret
	}
	applyInternalRelayHeaderWithSecret(ctx, jwtSecret, account, headers)
}

func applyInternalRelayHeaderWithSecret(ctx context.Context, jwtSecret string, account *Account, headers http.Header) {
	applyInternalRelayHeaderWithSigner(ctx, internalrelay.NewSigner(jwtSecret), account, headers)
}

func applyInternalRelayHeaderFromContext(ctx context.Context, account *Account, headers http.Header) {
	var signer *internalrelay.Signer
	if ctx != nil {
		signer, _ = ctx.Value(ctxkey.InternalRelaySigner).(*internalrelay.Signer)
	}
	applyInternalRelayHeaderWithSigner(ctx, signer, account, headers)
}

func applyInternalRelayHeaderWithSigner(ctx context.Context, signer *internalrelay.Signer, account *Account, headers http.Header) {
	if headers == nil {
		return
	}
	headers.Del(internalrelay.HeaderName)
	if account == nil || !account.hasValidInternalRelayConfiguration() {
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
	marker, err := signer.Sign(account.ID, parentRequestID, time.Now())
	if err != nil {
		return
	}
	headers.Set(internalrelay.HeaderName, marker)
}
