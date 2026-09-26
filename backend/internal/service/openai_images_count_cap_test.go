package service

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newImagesCountCapService(maxN int) *OpenAIGatewayService {
	cfg := &config.Config{}
	cfg.Gateway.ImagesMaxN = maxN
	return &OpenAIGatewayService{cfg: cfg}
}

// SEC-011：n 直接乘进按张计费金额，必须受 gateway.images_max_n 约束。
func TestOpenAIGatewayServiceParseOpenAIImagesRequest_CountCap(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		maxN    int
		body    string
		wantN   int
		wantErr string
	}{
		{name: "at cap", maxN: 10, body: `{"model":"gpt-image-2","n":10}`, wantN: 10},
		{name: "above cap", maxN: 10, body: `{"model":"gpt-image-2","n":11}`, wantErr: "n must be between 1 and 10"},
		{name: "far above cap", maxN: 10, body: `{"model":"gpt-image-2","n":1000}`, wantErr: "n must be between 1 and 10"},
		{name: "cap disabled", maxN: 0, body: `{"model":"gpt-image-2","n":1000}`, wantN: 1000},
		{name: "zero still rejected", maxN: 10, body: `{"model":"gpt-image-2","n":0}`, wantErr: "n must be a positive 32-bit integer"},
		{name: "default n unaffected", maxN: 10, body: `{"model":"gpt-image-2","prompt":"draw"}`, wantN: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newImagesCountCapService(tt.maxN)
			body := []byte(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = req

			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			if tt.wantErr != "" {
				require.Nil(t, parsed)
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantN, parsed.N)
		})
	}
}

func TestOpenAIGatewayServiceParseOpenAIImagesRequest_CountCapMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	build := func(t *testing.T, n string) ([]byte, string) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-2"))
		require.NoError(t, writer.WriteField("prompt", "replace background"))
		require.NoError(t, writer.WriteField("n", n))
		part, err := writer.CreateFormFile("image", "source.png")
		require.NoError(t, err)
		_, err = part.Write([]byte("png-bytes"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		return body.Bytes(), writer.FormDataContentType()
	}

	svc := newImagesCountCapService(10)
	for _, tt := range []struct {
		n       string
		wantErr string
	}{
		{n: "10"},
		{n: "11", wantErr: "n must be between 1 and 10"},
	} {
		t.Run("n="+tt.n, func(t *testing.T) {
			body, contentType := build(t, tt.n)
			req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
			req.Header.Set("Content-Type", contentType)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = req

			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			if tt.wantErr != "" {
				require.Nil(t, parsed)
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 10, parsed.N)
		})
	}
}

func TestOpenAIGatewayService_EstimateOpenAIImagesCost(t *testing.T) {
	unit := 0.04
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	newAPIKey := func(group *Group) *APIKey {
		gid := group.ID
		return &APIKey{ID: 1, UserID: 9, GroupID: &gid, Group: group}
	}
	svc := &OpenAIGatewayService{cfg: cfg, billingService: NewBillingService(cfg, nil)}

	t.Run("group unit price times n times image multiplier", func(t *testing.T) {
		group := &Group{ID: 5, Platform: PlatformOpenAI, RateMultiplier: 1.5, ImagePrice1K: &unit}
		parsed := &OpenAIImagesRequest{Model: "gpt-image-2", N: 10, SizeTier: ImageBillingSize1K}

		cost, ok := svc.EstimateOpenAIImagesCost(context.Background(), newAPIKey(group), parsed, "")

		require.True(t, ok)
		require.InDelta(t, unit*10*1.5, cost, 1e-9)
	})

	t.Run("independent image multiplier wins", func(t *testing.T) {
		group := &Group{ID: 5, Platform: PlatformOpenAI, RateMultiplier: 3, ImageRateIndependent: true, ImageRateMultiplier: 2, ImagePrice2K: &unit}
		parsed := &OpenAIImagesRequest{Model: "gpt-image-2", N: 3, SizeTier: ImageBillingSize2K}

		cost, ok := svc.EstimateOpenAIImagesCost(context.Background(), newAPIKey(group), parsed, "")

		require.True(t, ok)
		require.InDelta(t, unit*3*2, cost, 1e-9)
	})

	t.Run("price unknown fails open", func(t *testing.T) {
		group := &Group{ID: 5, Platform: PlatformOpenAI, RateMultiplier: 1}
		parsed := &OpenAIImagesRequest{Model: "totally-unknown-image-model", N: 10, SizeTier: ImageBillingSize1K}

		_, ok := svc.EstimateOpenAIImagesCost(context.Background(), newAPIKey(group), parsed, "")

		require.False(t, ok)
	})

	t.Run("nil billing service fails open", func(t *testing.T) {
		group := &Group{ID: 5, Platform: PlatformOpenAI, RateMultiplier: 1, ImagePrice1K: &unit}
		parsed := &OpenAIImagesRequest{Model: "gpt-image-2", N: 10, SizeTier: ImageBillingSize1K}

		_, ok := (&OpenAIGatewayService{cfg: cfg}).EstimateOpenAIImagesCost(context.Background(), newAPIKey(group), parsed, "")

		require.False(t, ok)
	})
}
