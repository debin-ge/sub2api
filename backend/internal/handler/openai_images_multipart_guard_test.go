//go:build unit

package handler

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIImagesMultipartGuardUpstream struct {
	service.HTTPUpstream
	calls int
}

func (u *openAIImagesMultipartGuardUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	return nil, fmt.Errorf("unexpected upstream call")
}

func newOpenAIImagesGuardHandler() (*OpenAIGatewayHandler, *openAIImagesMultipartGuardUpstream) {
	upstream := &openAIImagesMultipartGuardUpstream{}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	gatewayService := service.NewOpenAIGatewayService(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		nil,
		nil,
		nil,
		nil,
		upstream,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	return NewOpenAIGatewayHandler(
		gatewayService,
		service.NewConcurrencyService(nil),
		&service.BillingCacheService{},
		&service.APIKeyService{},
		nil,
		nil,
		nil,
		nil,
		cfg,
	), upstream
}

func TestOpenAIGatewayHandlerImages_RejectsDuplicateMultipartBillingFieldsBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		fields  [][2]string
		wantErr string
	}{
		{
			name: "model",
			fields: [][2]string{
				{"model", "gpt-image-1.5"},
				{"model", "gpt-image-2"},
				{"size", "1024x1024"},
			},
			wantErr: "duplicate multipart model fields are not allowed",
		},
		{
			name: "size",
			fields: [][2]string{
				{"model", "gpt-image-2"},
				{"size", "1024x1024"},
				{"size", "3840x2160"},
			},
			wantErr: "duplicate multipart size fields are not allowed",
		},
		{
			name: "n",
			fields: [][2]string{
				{"model", "gpt-image-2"},
				{"size", "1024x1024"},
				{"n", "1"},
				{"n", "4"},
			},
			wantErr: "duplicate multipart n fields are not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			for _, field := range tt.fields {
				require.NoError(t, writer.WriteField(field[0], field[1]))
			}
			require.NoError(t, writer.Close())

			h, upstream := newOpenAIImagesGuardHandler()

			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body.Bytes()))
			req.Header.Set("Content-Type", writer.FormDataContentType())
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = req
			groupID := int64(901)
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
				ID:      902,
				GroupID: &groupID,
				Group: &service.Group{
					ID:                   groupID,
					AllowImageGeneration: true,
				},
				User: &service.User{ID: 903},
			})
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 903})

			h.Images(c)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
			require.Equal(t, tt.wantErr, gjson.GetBytes(rec.Body.Bytes(), "error.message").String())
			require.Zero(t, upstream.calls)
		})
	}
}

func TestOpenAIGatewayHandlerImages_RejectsDuplicateJSONBillingFieldsBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "model",
			body:    `{"model":"gpt-image-1.5","model":"gpt-image-2","size":"1024x1024","n":1}`,
			wantErr: "duplicate top-level model fields are not allowed",
		},
		{
			name:    "size",
			body:    `{"model":"gpt-image-2","size":"1024x1024","size":"3840x2160","n":1}`,
			wantErr: "duplicate top-level size fields are not allowed",
		},
		{
			name:    "n",
			body:    `{"model":"gpt-image-2","size":"1024x1024","n":1,"n":4}`,
			wantErr: "duplicate top-level n fields are not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, upstream := newOpenAIImagesGuardHandler()

			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = req
			groupID := int64(911)
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
				ID:      912,
				GroupID: &groupID,
				Group: &service.Group{
					ID:                   groupID,
					AllowImageGeneration: true,
				},
				User: &service.User{ID: 913},
			})
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 913})

			h.Images(c)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
			require.Equal(t, tt.wantErr, gjson.GetBytes(rec.Body.Bytes(), "error.message").String())
			require.Zero(t, upstream.calls)
		})
	}
}
