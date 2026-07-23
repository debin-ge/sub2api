package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAuthHandler_ValidateAffiliateCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		enabled    bool
		code       string
		codeOwners map[string]int64
		valid      bool
		errorCode  string
	}{
		{
			name:       "valid",
			enabled:    true,
			code:       "AFF123",
			codeOwners: map[string]int64{"AFF123": 7},
			valid:      true,
		},
		{
			name:       "invalid",
			enabled:    true,
			code:       "UNKNOWN",
			codeOwners: map[string]int64{},
			errorCode:  "AFFILIATE_CODE_INVALID",
		},
		{
			name:       "disabled",
			enabled:    false,
			code:       "AFF123",
			codeOwners: map[string]int64{"AFF123": 7},
			errorCode:  "AFFILIATE_DISABLED",
		},
		{
			name:       "disabled takes precedence over malformed code",
			enabled:    false,
			code:       "!",
			codeOwners: map[string]int64{},
			errorCode:  "AFFILIATE_DISABLED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			settingService := service.NewSettingService(
				&oauthPendingFlowSettingRepoStub{values: map[string]string{
					service.SettingKeyAffiliateEnabled: boolSettingValue(tt.enabled),
				}},
				cfg,
			)
			affiliateService := service.NewAffiliateService(
				newOAuthEmailAffiliateRepoStub(tt.codeOwners),
				settingService,
				nil,
				nil,
			)
			authService := service.NewAuthService(
				nil,
				nil,
				nil,
				nil,
				cfg,
				settingService,
				nil,
				nil,
				nil,
				nil,
				nil,
				affiliateService,
				nil,
				nil,
				nil,
				nil,
			)
			handler := NewAuthHandler(cfg, authService, nil, settingService, nil, nil, nil, nil)

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			body, err := json.Marshal(map[string]string{"code": tt.code})
			require.NoError(t, err)
			c.Request = httptest.NewRequest(
				http.MethodPost,
				"/api/v1/auth/validate-affiliate-code",
				bytes.NewReader(body),
			)
			c.Request.Header.Set("Content-Type", "application/json")

			handler.ValidateAffiliateCode(c)

			require.Equal(t, http.StatusOK, recorder.Code)
			var responseBody struct {
				Data struct {
					Valid     bool   `json:"valid"`
					ErrorCode string `json:"error_code"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &responseBody))
			require.Equal(t, tt.valid, responseBody.Data.Valid)
			require.Equal(t, tt.errorCode, responseBody.Data.ErrorCode)
		})
	}
}

func TestAuthHandler_ValidateAffiliateCodeServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	settingService := service.NewSettingService(
		&oauthPendingFlowSettingRepoStub{values: map[string]string{
			service.SettingKeyAffiliateEnabled: "true",
		}},
		cfg,
	)
	authService := service.NewAuthService(
		nil,
		nil,
		nil,
		nil,
		cfg,
		settingService,
		nil,
		nil,
		nil,
		nil,
		nil,
		service.NewAffiliateService(nil, settingService, nil, nil),
		nil,
		nil,
		nil,
		nil,
	)
	handler := NewAuthHandler(cfg, authService, nil, settingService, nil, nil, nil, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/validate-affiliate-code",
		bytes.NewBufferString(`{"code":"AFF123"}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.ValidateAffiliateCode(c)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"reason":"SERVICE_UNAVAILABLE"`)
}
