package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsImageGenerationIntent(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		model    string
		body     []byte
		want     bool
	}{
		{
			name:     "images endpoint",
			endpoint: "/v1/images/generations",
			body:     []byte(`{"model":"gpt-image-2"}`),
			want:     true,
		},
		{
			name:     "image model",
			endpoint: "/v1/responses",
			model:    "gpt-image-2",
			body:     []byte(`{"model":"gpt-image-2"}`),
			want:     true,
		},
		{
			name:     "image tool",
			endpoint: "/v1/responses",
			model:    "gpt-5.4",
			body:     []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation"}]}`),
			want:     true,
		},
		{
			name:     "image tool choice",
			endpoint: "/v1/responses",
			model:    "gpt-5.4",
			body:     []byte(`{"model":"gpt-5.4","tool_choice":{"type":"image_generation"}}`),
			want:     true,
		},
		{
			name:     "namespace image_gen tool choice",
			endpoint: "/v1/responses",
			model:    "gpt-5.5",
			body:     []byte(`{"model":"gpt-5.5","tool_choice":{"type":"namespace","name":"image_gen"}}`),
			want:     true,
		},
		{
			name:     "custom imagegen function tool choice is not image intent",
			endpoint: "/v1/responses",
			model:    "gpt-5.5",
			body:     []byte(`{"model":"gpt-5.5","tool_choice":{"function":{"name":"imagegen"}}}`),
			want:     false,
		},
		{
			name:     "required tool choice alone is text",
			endpoint: "/v1/responses",
			model:    "gpt-5.4",
			body:     []byte(`{"model":"gpt-5.4","tool_choice":"required"}`),
			want:     false,
		},
		{
			name:     "text only gpt 5.4",
			endpoint: "/v1/responses",
			model:    "gpt-5.4",
			body:     []byte(`{"model":"gpt-5.4","input":"write code"}`),
			want:     false,
		},
		{
			name:     "namespace image_gen tool in top-level tools",
			endpoint: "/v1/responses",
			model:    "gpt-5.5",
			body:     []byte(`{"model":"gpt-5.5","tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}]}`),
			want:     true,
		},
		{
			name:     "custom namespace with nested imagegen function is not image intent",
			endpoint: "/v1/responses",
			model:    "gpt-5.5",
			body:     []byte(`{"model":"gpt-5.5","tools":[{"type":"namespace","name":"media_tools","tools":[{"type":"function","name":"imagegen"}]}]}`),
			want:     false,
		},
		{
			name:     "namespace image_gen in input additional_tools (Responses Lite)",
			endpoint: "/v1/responses",
			model:    "gpt-5.5",
			body:     []byte(`{"model":"gpt-5.5","input":[{"type":"additional_tools","role":"developer","tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}]}]}`),
			want:     true,
		},
		{
			name:     "non-image namespace tool is not flagged",
			endpoint: "/v1/responses",
			model:    "gpt-5.5",
			body:     []byte(`{"model":"gpt-5.5","tools":[{"type":"namespace","name":"code_tools","tools":[{"type":"function","name":"run"}]}]}`),
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsImageGenerationIntent(tt.endpoint, tt.model, tt.body))
		})
	}
}

func TestIsImageGenerationIntentJSONSemantics(t *testing.T) {
	largeInput := strings.Repeat("x", 1<<20)
	tests := []struct {
		name     string
		endpoint string
		body     []byte
		want     bool
	}{
		{
			name:     "chat body image model",
			endpoint: "/v1/chat/completions",
			body:     []byte(`{"model":"gpt-image-2"}`),
			want:     true,
		},
		{
			name:     "large responses input with trailing namespace tool choice",
			endpoint: "/v1/responses",
			body:     []byte(`{"model":"gpt-5.5","input":"` + largeInput + `","tool_choice":{"type":"namespace","name":"image_gen"}}`),
			want:     true,
		},
		{
			name:     "invalid json with image tool",
			endpoint: "/v1/responses",
			body:     []byte(`{"tools":[{"type":"image_generation"}]`),
			want:     false,
		},
		{
			name:     "duplicate model conservatively detects any image value",
			endpoint: "/v1/responses",
			body:     []byte(`{"model":"gpt-5.5","model":"gpt-image-2"}`),
			want:     true,
		},
		{
			name:     "duplicate null model conservatively detects image value",
			endpoint: "/v1/responses",
			body:     []byte(`{"model":null,"model":"gpt-image-2"}`),
			want:     true,
		},
		{
			name:     "duplicate tools conservatively detects image declaration",
			endpoint: "/v1/responses",
			body:     []byte(`{"tools":[],"tools":[{"type":"image_generation"}]}`),
			want:     true,
		},
		{
			name:     "duplicate input conservatively detects image declaration",
			endpoint: "/v1/responses",
			body:     []byte(`{"input":[],"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"image_gen"}]}]}`),
			want:     true,
		},
		{
			name:     "duplicate tool choice conservatively detects image selection",
			endpoint: "/v1/responses",
			body:     []byte(`{"tool_choice":"required","tool_choice":{"type":"image_generation"}}`),
			want:     true,
		},
		{
			name:     "escaped top level key",
			endpoint: "/v1/responses",
			body:     []byte(`{"tool_\u0063hoice":{"type":"image_generation"}}`),
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsImageGenerationIntent(tt.endpoint, "gpt-5.5", tt.body))
		})
	}
}

func TestIsImageGenerationIntentMap_NamespaceImageGen(t *testing.T) {
	tests := []struct {
		name    string
		reqBody map[string]any
		want    bool
	}{
		{
			name: "top-level namespace image_gen",
			reqBody: map[string]any{
				"model": "gpt-5.5",
				"tools": []any{
					map[string]any{"type": "namespace", "name": "image_gen", "tools": []any{
						map[string]any{"type": "function", "name": "imagegen"},
					}},
				},
			},
			want: true,
		},
		{
			name: "additional_tools in input",
			reqBody: map[string]any{
				"model": "gpt-5.5",
				"input": []any{
					map[string]any{
						"type": "additional_tools",
						"tools": []any{
							map[string]any{"type": "namespace", "name": "image_gen"},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "custom namespace with nested imagegen function is not image intent",
			reqBody: map[string]any{
				"model": "gpt-5.5",
				"tools": []any{
					map[string]any{
						"type": "namespace",
						"name": "media_tools",
						"tools": []any{
							map[string]any{"type": "function", "name": "imagegen"},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "namespace image_gen tool choice",
			reqBody: map[string]any{
				"model":       "gpt-5.5",
				"tool_choice": map[string]any{"type": "namespace", "name": "image_gen"},
			},
			want: true,
		},
		{
			name: "custom imagegen function tool choice is not image intent",
			reqBody: map[string]any{
				"model": "gpt-5.5",
				"tool_choice": map[string]any{
					"function": map[string]any{"name": "imagegen"},
				},
			},
			want: false,
		},
		{
			name: "non-image namespace not flagged",
			reqBody: map[string]any{
				"model": "gpt-5.5",
				"tools": []any{
					map[string]any{"type": "namespace", "name": "code_tools"},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsImageGenerationIntentMap("/v1/responses", "gpt-5.5", tt.reqBody))
		})
	}
}

func TestResolveOpenAIResponsesImageBillingConfigUsesCurrentBodyModel(t *testing.T) {
	imageModel, imageSize, err := resolveOpenAIResponsesImageBillingConfigFromBody(
		[]byte(`{"model":"mapped-image-model","tools":[{"type":"image_generation","size":"1024x1024"}]}`),
		"requested-model",
	)
	require.NoError(t, err)
	require.Equal(t, "mapped-image-model", imageModel)
	require.Equal(t, "1K", imageSize)
}

func TestResolveOpenAIResponsesImageBillingConfigToolModelWins(t *testing.T) {
	imageModel, imageSize, err := resolveOpenAIResponsesImageBillingConfigFromBody(
		[]byte(`{"model":"mapped-text-model","tools":[{"type":"image_generation","model":"gpt-image-2","size":"1536x1024"}]}`),
		"requested-model",
	)
	require.NoError(t, err)
	require.Equal(t, "gpt-image-2", imageModel)
	require.Equal(t, "2K", imageSize)
}

func TestResolveOpenAIResponsesImageBillingConfigUsesResponsesLiteToolIdentity(t *testing.T) {
	rawCfg, err := resolveOpenAIResponsesImageBillingConfigDetailedFromBody(
		[]byte(`{
			"model":"gpt-5.4",
			"input":[{
				"type":"additional_tools",
				"tools":[{"type":"image_generation","model":"nested-image-model","size":"3840x2160"}]
			}]
		}`),
		"fallback-model",
	)
	require.NoError(t, err)
	require.Equal(t, "nested-image-model", rawCfg.Model)
	require.Equal(t, "4K", rawCfg.SizeTier)
	require.Equal(t, "3840x2160", rawCfg.InputSize)

	mapCfg, err := resolveOpenAIResponsesImageBillingConfigDetailed(
		map[string]any{
			"model": "gpt-5.4",
			"input": []any{
				map[string]any{
					"type": "additional_tools",
					"tools": []any{
						map[string]any{
							"type":  "image_generation",
							"model": "nested-map-image-model",
							"size":  "2048x1152",
						},
					},
				},
			},
		},
		"fallback-model",
	)
	require.NoError(t, err)
	require.Equal(t, "nested-map-image-model", mapCfg.Model)
	require.Equal(t, "2K", mapCfg.SizeTier)
	require.Equal(t, "2048x1152", mapCfg.InputSize)
}

func TestResolveOpenAIResponsesImageBillingConfigRejectsMultipleResponsesLiteImageTools(t *testing.T) {
	tests := []struct {
		name    string
		rawBody []byte
		mapBody map[string]any
	}{
		{
			name: "top level and nested",
			rawBody: []byte(`{
				"model":"gpt-5.4",
				"tools":[{"type":"image_generation","model":"priced-image-model"}],
				"input":[{
					"type":"additional_tools",
					"tools":[{"type":"image_generation","model":"unknown-image-model"}]
				}]
			}`),
			mapBody: map[string]any{
				"model": "gpt-5.4",
				"tools": []any{
					map[string]any{"type": "image_generation", "model": "priced-image-model"},
				},
				"input": []any{
					map[string]any{
						"type": "additional_tools",
						"tools": []any{
							map[string]any{"type": "image_generation", "model": "unknown-image-model"},
						},
					},
				},
			},
		},
		{
			name: "two nested",
			rawBody: []byte(`{
				"model":"gpt-5.4",
				"input":[{
					"type":"additional_tools",
					"tools":[
						{"type":"image_generation","model":"priced-image-model"},
						{"type":"image_generation","model":"unknown-image-model"}
					]
				}]
			}`),
			mapBody: map[string]any{
				"model": "gpt-5.4",
				"input": []any{
					map[string]any{
						"type": "additional_tools",
						"tools": []any{
							map[string]any{"type": "image_generation", "model": "priced-image-model"},
							map[string]any{"type": "image_generation", "model": "unknown-image-model"},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveOpenAIResponsesImageBillingConfigDetailedFromBody(tt.rawBody, "gpt-5.4")
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Contains(t, err.Error(), "multiple image_generation tools")

			_, err = resolveOpenAIResponsesImageBillingConfigDetailed(tt.mapBody, "gpt-5.4")
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Contains(t, err.Error(), "multiple image_generation tools")
		})
	}
}

func TestResolveOpenAIResponsesImageBillingConfigRejectsMultipleImageTools(t *testing.T) {
	rawBody := []byte(`{
		"model":"gpt-5.4",
		"tools":[
			{"type":"image_generation","model":"priced-image-model","size":"1024x1024"},
			{"type":"image_generation","model":"unknown-image-model","size":"3840x2160"}
		]
	}`)
	_, err := resolveOpenAIResponsesImageBillingConfigDetailedFromBody(rawBody, "gpt-5.4")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Contains(t, err.Error(), "multiple image_generation tools")

	mapBody := map[string]any{
		"model": "gpt-5.4",
		"tools": []any{
			map[string]any{"type": "image_generation", "model": "priced-image-model", "size": "1024x1024"},
			map[string]any{"type": "image_generation", "model": "unknown-image-model", "size": "3840x2160"},
		},
	}
	_, err = resolveOpenAIResponsesImageBillingConfigDetailed(mapBody, "gpt-5.4")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Contains(t, err.Error(), "multiple image_generation tools")
}

func TestResolveOpenAIResponsesImageBillingConfigRejectsDuplicateIdentityFields(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{
			name: "tools",
			body: []byte(`{"model":"gpt-5.4","tools":[{"type":"function"}],"tools":[{"type":"image_generation","model":"unknown-image-model"}]}`),
		},
		{
			name: "model",
			body: []byte(`{"model":"gpt-5.4","model":"unknown-image-model","tools":[{"type":"image_generation"}]}`),
		},
		{
			name: "size",
			body: []byte(`{"model":"gpt-image-2","size":"1024x1024","size":"3840x2160","tools":[{"type":"image_generation"}]}`),
		},
		{
			name: "tool choice",
			body: []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation"}],"tool_choice":"auto","tool_choice":{"type":"image_generation"}}`),
		},
		{
			name: "input",
			body: []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation"}],"input":"first","input":"second"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveOpenAIResponsesImageBillingConfigDetailedFromBody(tt.body, "gpt-5.4")
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Contains(t, err.Error(), "duplicate top-level")
		})
	}
}

func TestValidateUniqueOpenAIResponsesBillingFieldsRejectsNestedIdentityDuplicates(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		wantPath string
	}{
		{
			name:     "tool type hides image intent",
			body:     []byte(`{"model":"gpt-5.4","tools":[{"type":"function","type":"image_generation","model":"unknown-image-model"}]}`),
			wantPath: "tools[0].type",
		},
		{
			name:     "tool model changes billed sku",
			body:     []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"priced-image-model","model":"unknown-image-model"}]}`),
			wantPath: "tools[0].model",
		},
		{
			name:     "escaped tool size changes billed tier",
			body:     []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","size":"1K","s\u0069ze":"4K"}]}`),
			wantPath: "tools[0].size",
		},
		{
			name:     "responses lite nested tool model",
			body:     []byte(`{"model":"gpt-5.4","input":[{"type":"additional_tools","tools":[{"type":"image_generation","model":"priced-image-model","model":"unknown-image-model"}]}]}`),
			wantPath: "input[0].tools[0].model",
		},
		{
			name:     "nested tool choice type",
			body:     []byte(`{"model":"gpt-5.4","tool_choice":{"type":"function","type":"image_generation"}}`),
			wantPath: "tool_choice.type",
		},
		{
			name:     "service tier changes settlement multiplier",
			body:     []byte(`{"model":"gpt-5.4","service_tier":"standard","service_tier":"priority"}`),
			wantPath: "service_tier",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUniqueOpenAIResponsesBillingFields(tt.body)
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Contains(t, err.Error(), tt.wantPath)
		})
	}
}

func TestValidateUniqueOpenAIResponsesBillingFieldsRejectsUnbillableValues(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		wantPath string
	}{
		{
			name:     "unknown root size",
			body:     []byte(`{"model":"gpt-image-2","size":"ultra","tools":[{"type":"image_generation"}]}`),
			wantPath: "size",
		},
		{
			name:     "unknown nested size",
			body:     []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"gpt-image-2","size":"ultra"}]}`),
			wantPath: "tools[0].size",
		},
		{
			name:     "non string nested size",
			body:     []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"gpt-image-2","size":2048}]}`),
			wantPath: "tools[0].size",
		},
		{
			name:     "nested model null character",
			body:     []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"gpt-image-2\u0000future"}]}`),
			wantPath: "tools[0].model",
		},
		{
			name:     "nested model must be string",
			body:     []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":null}]}`),
			wantPath: "tools[0].model",
		},
		{
			name:     "responses lite nested model must be string",
			body:     []byte(`{"model":"gpt-5.4","input":[{"type":"additional_tools","tools":[{"type":"image_generation","model":42}]}]}`),
			wantPath: "input[0].tools[0].model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUniqueOpenAIResponsesBillingFields(tt.body)
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Contains(t, err.Error(), tt.wantPath)
		})
	}

	for _, body := range [][]byte{
		[]byte(`{"model":"gpt-image-2","size":"","tools":[{"type":"image_generation"}]}`),
		[]byte(`{"model":"gpt-image-2","size":"auto","tools":[{"type":"image_generation"}]}`),
		[]byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","size":"auto"}]}`),
		[]byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","size":"3840x2160"}]}`),
	} {
		require.NoError(t, ValidateUniqueOpenAIResponsesBillingFields(body))
	}
}

func TestValidateUniqueOpenAIResponsesBillingFieldsDoesNotInspectFunctionParameterSchema(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.4",
		"tools":[{
			"type":"function",
			"name":"render_asset",
			"parameters":{
				"type":"object",
				"properties":{
					"model":{"type":"string"},
					"size":{"type":"string","default":"ultra"}
				}
			}
		}]
	}`)

	require.NoError(t, ValidateUniqueOpenAIResponsesBillingFields(body))
}

func TestResolveOpenAIResponsesImageBillingConfigMapRejectsUnbillableSize(t *testing.T) {
	_, err := resolveOpenAIResponsesImageBillingConfigDetailed(
		map[string]any{
			"model": "gpt-5.4",
			"tools": []any{
				map[string]any{"type": "image_generation", "model": "gpt-image-2", "size": "ultra"},
			},
		},
		"gpt-5.4",
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Contains(t, err.Error(), "size")
}

func TestResolveOpenAIResponsesImageBillingConfigMapRejectsNonStringToolIdentity(t *testing.T) {
	for _, tool := range []map[string]any{
		{"type": "image_generation", "model": nil},
		{"type": "image_generation", "model": 7},
		{"type": "image_generation", "model": "gpt-image-2", "size": 2048},
	} {
		_, err := resolveOpenAIResponsesImageBillingConfigDetailed(
			map[string]any{
				"model": "gpt-5.4",
				"tools": []any{tool},
			},
			"gpt-5.4",
		)
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
	}
}

func TestIsExplicitImageGenerationIntentScansDuplicateRootFields(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","tools":[{"type":"function","name":"safe"}],"tools":[{"type":"image_generation","model":"unknown-image-model"}]}`)
	require.True(t, IsExplicitImageGenerationIntent(openAIResponsesEndpoint, "gpt-5.4", body))
}

func TestResolveOpenAIResponsesImageBillingConfigFromBodyIgnoresUnrelatedLargeInput(t *testing.T) {
	cfg, err := resolveOpenAIResponsesImageBillingConfigDetailedFromBody(
		[]byte(`{"model":"mapped-text-model","tools":[{"type":"image_generation","model":"gpt-image-2","size":"2048x1152"}],"input":[{"type":"message","content":[{"type":"input_text","text":"hi","nonce":1e1000000}]}]}`),
		"requested-model",
	)
	require.NoError(t, err)
	require.Equal(t, "gpt-image-2", cfg.Model)
	require.Equal(t, "2K", cfg.SizeTier)
	require.Equal(t, "2048x1152", cfg.InputSize)
	require.True(t, cfg.NativeTool)
}

func TestResolveOpenAIResponsesImageBillingConfigRejectsNonStringNativeToolModel(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":null}]}`),
		[]byte(`{"model":"gpt-5.4","input":[{"type":"additional_tools","tools":[{"type":"image_generation","model":7}]}]}`),
	} {
		_, err := ResolveOpenAIResponsesImageBillingConfigFromBody(body, "gpt-5.4")
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.ErrorContains(t, err, "model")
	}
}

func TestResolveOpenAIResponsesImageBillingConfigSupportsOfficialAndCustomSizes(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		wantTier string
	}{
		{
			name:     "official 2k landscape",
			body:     []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"gpt-image-2","size":"2048x1152"}]}`),
			wantTier: "2K",
		},
		{
			name:     "official 4k landscape",
			body:     []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"gpt-image-2","size":"3840x2160"}]}`),
			wantTier: "4K",
		},
		{
			name:     "custom valid 2k",
			body:     []byte(`{"model":"gpt-5.5","tools":[{"type":"image_generation","model":"gpt-image-2","size":"1280x768"}]}`),
			wantTier: "2K",
		},
		{
			name:     "default image tool model supports flexible size",
			body:     []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","size":"2048x1152"}]}`),
			wantTier: "2K",
		},
		{
			name:     "top level image size is moved into billing",
			body:     []byte(`{"model":"gpt-image-2","size":"2048x2048","tools":[{"type":"image_generation","model":"gpt-image-2"}]}`),
			wantTier: "2K",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageModel, imageSize, err := resolveOpenAIResponsesImageBillingConfigFromBody(tt.body, "requested-model")
			require.NoError(t, err)
			require.NotEmpty(t, imageModel)
			require.Equal(t, tt.wantTier, imageSize)
		})
	}
}

func TestResolveOpenAIResponsesImageBillingConfigDoesNotRejectUnknownSizes(t *testing.T) {
	imageModel, imageSize, err := resolveOpenAIResponsesImageBillingConfigFromBody(
		[]byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"gpt-image-1.5","size":"2048x1152"}]}`),
		"requested-model",
	)
	require.NoError(t, err)
	require.Equal(t, "gpt-image-1.5", imageModel)
	require.Equal(t, "2K", imageSize)
}

func TestOpenAIImageOutputCounterDeduplicatesFinalImages(t *testing.T) {
	counter := newOpenAIImageOutputCounter()
	counter.AddSSEData([]byte(`{"type":"response.image_generation_call.partial_image","partial_image_b64":"abc"}`))
	counter.AddSSEData([]byte(`{"type":"response.output_item.done","item":{"id":"ig_1","type":"image_generation_call","result":"final-a","size":"1024x1024"}}`))
	counter.AddSSEData([]byte(`{"type":"response.completed","response":{"output":[{"id":"ig_1","type":"image_generation_call","result":"final-a"},{"id":"ig_2","type":"image_generation_call","result":"final-b","size":"3840x2160"}]}}`))
	require.Equal(t, 2, counter.Count())
	require.Equal(t, []string{"1024x1024", "3840x2160"}, counter.Sizes())
}

func TestOpenAIImageOutputCounterCountsImagesAPIStreamShapes(t *testing.T) {
	counter := newOpenAIImageOutputCounter()
	counter.AddSSEData([]byte(`{"type":"image_generation.completed","id":"ig_complete","b64_json":"final-a"}`))
	counter.AddSSEData([]byte(`{"type":"response.output_item.done","item":{"id":"ig_item","type":"image_generation_call","result":"final-b"}}`))
	counter.AddSSEData([]byte(`{"type":"response.completed","response":{"output":[{"id":"ig_done","type":"image_generation_call","result":"final-c"}]}}`))
	require.Equal(t, 3, counter.Count())

	dataCounter := newOpenAIImageOutputCounter()
	dataCounter.AddSSEData([]byte(`{"data":[{"b64_json":"a"},{"b64_json":"b"}]}`))
	dataCounter.AddSSEData([]byte(`{"data":[{"b64_json":"a"},{"b64_json":"b"},{"b64_json":"c"}]}`))
	require.Equal(t, 3, dataCounter.Count())
}

func TestOpenAIImageOutputCounterCountsMultilineSSEDataPayload(t *testing.T) {
	counter := newOpenAIImageOutputCounter()
	counter.AddSSEData([]byte("{\"type\":\"image_generation.completed\",\n\"b64_json\":\"final-a\"}"))
	require.Equal(t, 1, counter.Count())
}

func TestOpenAIImageOutputCounterCountsMultilineSSEBodyPayload(t *testing.T) {
	counter := newOpenAIImageOutputCounter()
	counter.AddSSEBody(
		"data: {\"type\":\"image_generation.completed\",\n" +
			"data: \"b64_json\":\"final-a\"}\n\n" +
			"data: [DONE]\n\n",
	)
	require.Equal(t, 1, counter.Count())
}

func TestOpenAIImageOutputCounterFallsBackForInvalidMultilineSSEBody(t *testing.T) {
	counter := newOpenAIImageOutputCounter()
	counter.AddSSEBody(
		"data: {\"type\":\"image_generation.completed\",\"b64_json\":\"final-a\"}\n" +
			"data: {\"type\":\"image_generation.completed\",\"b64_json\":\"final-b\"}\n\n",
	)
	require.Equal(t, 2, counter.Count())
}

func TestCollectOpenAIResponseImageOutputSizesFromJSONBytes(t *testing.T) {
	body := []byte(`{
		"output": [
			{"id":"ig_1","type":"image_generation_call","result":"final-a","size":"3840x2160"},
			{"id":"ig_2","type":"image_generation_call","result":"final-b","size":"1024x1024"}
		]
	}`)

	require.Equal(t, 2, countOpenAIResponseImageOutputsFromJSONBytes(body))
	require.Equal(t, []string{"3840x2160", "1024x1024"}, collectOpenAIResponseImageOutputSizesFromJSONBytes(body))
}

func TestCollectOpenAIResponseImageOutputSizesFromImagesAPIData(t *testing.T) {
	body := []byte(`{
		"data": [
			{"b64_json":"final-a","size":"2048x1152"},
			{"b64_json":"final-b","size":"2048x1152"}
		]
	}`)

	require.Equal(t, 2, countOpenAIResponseImageOutputsFromJSONBytes(body))
	require.Equal(t, []string{"2048x1152", "2048x1152"}, collectOpenAIResponseImageOutputSizesFromJSONBytes(body))
}

func TestCollectOpenAIImageOutputSizesFromSSEBody(t *testing.T) {
	body := "data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"result\":\"final-a\",\"size\":\"3840x2160\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"result\":\"final-a\"},{\"id\":\"ig_2\",\"type\":\"image_generation_call\",\"result\":\"final-b\",\"size\":\"1024x1024\"}]}}\n\n" +
		"data: [DONE]\n\n"

	require.Equal(t, 2, countOpenAIImageOutputsFromSSEBody(body))
	require.Equal(t, []string{"3840x2160", "1024x1024"}, collectOpenAIImageOutputSizesFromSSEBody(body))
}
