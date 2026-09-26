package service

import (
	"bytes"
	"mime/multipart"
	"testing"

	"github.com/stretchr/testify/require"
)

// SEC-011：Grok 媒体请求的 n 与 /v1/images 共用 gateway.images_max_n 上限。
func TestValidateGrokMediaBillingFieldsForEndpointWithMaxN(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			maxN    int
			body    string
			wantErr string
		}{
			{name: "at cap", maxN: 10, body: `{"model":"grok-imagine-image","prompt":"x","n":10}`},
			{name: "above cap", maxN: 10, body: `{"model":"grok-imagine-image","prompt":"x","n":11}`, wantErr: "n must be between 1 and 10"},
			{name: "cap disabled", maxN: 0, body: `{"model":"grok-imagine-image","prompt":"x","n":500}`},
			{name: "legacy entry point stays uncapped", maxN: -1, body: `{"model":"grok-imagine-image","prompt":"x","n":500}`},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var err error
				if tt.maxN < 0 {
					err = ValidateGrokMediaBillingFieldsForEndpoint(GrokMediaEndpointImagesGenerations, "application/json", []byte(tt.body))
				} else {
					err = ValidateGrokMediaBillingFieldsForEndpointWithMaxN(GrokMediaEndpointImagesGenerations, "application/json", []byte(tt.body), tt.maxN)
				}
				if tt.wantErr != "" {
					require.ErrorContains(t, err, tt.wantErr)
					return
				}
				require.NoError(t, err)
			})
		}
	})

	t.Run("multipart", func(t *testing.T) {
		build := func(t *testing.T, n string) ([]byte, string) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("model", "grok-imagine-image"))
			require.NoError(t, writer.WriteField("prompt", "x"))
			require.NoError(t, writer.WriteField("n", n))
			require.NoError(t, writer.Close())
			return body.Bytes(), writer.FormDataContentType()
		}

		body, contentType := build(t, "10")
		require.NoError(t, ValidateGrokMediaBillingFieldsForEndpointWithMaxN(GrokMediaEndpointImagesEdits, contentType, body, 10))

		body, contentType = build(t, "11")
		require.ErrorContains(t,
			ValidateGrokMediaBillingFieldsForEndpointWithMaxN(GrokMediaEndpointImagesEdits, contentType, body, 10),
			"n must be between 1 and 10",
		)
	})
}
