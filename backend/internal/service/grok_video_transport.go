package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *OpenAIGatewayService) FetchGrokVideoStatus(ctx context.Context, account *Account, id string) (*GrokVideoHTTPResponse, error) {
	target, err := buildGrokMediaURL(account, s.cfg, GrokMediaEndpointVideoStatus, id)
	if err != nil {
		return nil, err
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(req.Header)
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		delay := time.Duration(0)
		if sec, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil {
			delay = time.Duration(sec) * time.Second
		} else if date, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil {
			delay = time.Until(date)
			if delay < 0 {
				delay = 0
			}
		}
		return nil, &GrokVideoHTTPError{Status: resp.StatusCode, RetryAfter: delay}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, fmt.Errorf("grok video status exceeds size limit")
	}
	return &GrokVideoHTTPResponse{Body: body, Status: resp.StatusCode, Header: resp.Header.Clone()}, nil
}
func (s *OpenAIGatewayService) WriteTrackedGrokVideoStatus(c *gin.Context, id string, body []byte) {
	body = rewriteGrokMediaVideoContentURLs(body, id, grokMediaContentProxyURL(c, id))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/json", body)
}
func (s *OpenAIGatewayService) ForwardTrackedGrokVideoContent(ctx context.Context, c *gin.Context, account *Account, id string, body []byte) error {
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return err
	}
	_, err = s.forwardGrokMediaVideoContent(ctx, c, account, token, id, time.Now(), body)
	return err
}
