package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	openAIResponsesEndpoint          = "/v1/responses"
	openAIResponsesCompactEndpoint   = "/v1/responses/compact"
	responsesLiteHeader              = "X-OpenAI-Internal-Codex-Responses-Lite"
	responsesLiteHeaderKey           = "x-openai-internal-codex-responses-lite"
	responsesLiteWSMetadataKey       = "ws_request_header_x_openai_internal_codex_responses_lite"
	imageGenerationPermissionMessage = "Image generation is not enabled for this group"
)

func isOpenAIResponsesLiteHeader(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func isOpenAIResponsesLiteWebSocketPayload(body []byte) bool {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	return isOpenAIResponsesLiteHeader(gjson.GetBytes(body, "client_metadata."+responsesLiteWSMetadataKey).String())
}

// ImageGenerationPermissionMessage returns the stable end-user error text for disabled groups.
func ImageGenerationPermissionMessage() string {
	return imageGenerationPermissionMessage
}

// GroupAllowsImageGeneration preserves ungrouped-key behavior and enforces the flag when a group is present.
func GroupAllowsImageGeneration(group *Group) bool {
	return group == nil || group.AllowImageGeneration
}

// IsImageGenerationIntent classifies requests that can produce generated images.
func IsImageGenerationIntent(endpoint string, requestedModel string, body []byte) bool {
	if IsImageGenerationEndpoint(endpoint) {
		return true
	}
	if isOpenAIImageGenerationModel(requestedModel) {
		return true
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}

	imageIntent := false
	parseRawJSONView(body).ForEach(func(key, value gjson.Result) bool {
		switch key.Str {
		case "model":
			imageIntent = imageIntent || isOpenAIImageGenerationModel(strings.TrimSpace(value.String()))
		case "tools":
			imageIntent = imageIntent || openAIJSONToolsContainImageGeneration(value)
		case "input":
			imageIntent = imageIntent || openAIJSONInputContainsImageGenTool(value)
		case "tool_choice":
			imageIntent = imageIntent || openAIJSONToolChoiceSelectsImageGeneration(value)
		}
		return !imageIntent
	})
	return imageIntent
}

// IsExplicitImageGenerationIntent 仅检测原生 image_generation 工具、图片模型和显式 tool_choice，
// 不检测被动的 image_gen namespace 声明。用于 capability 路由决策——被动 namespace 不应
// 强制要求原生 Responses 能力，否则 Chat Completions-only 账号会被误过滤（#4476）。
func IsExplicitImageGenerationIntent(endpoint string, requestedModel string, body []byte) bool {
	if IsImageGenerationEndpoint(endpoint) || isOpenAIImageGenerationModel(requestedModel) {
		return true
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	imageIntent := false
	parseRawJSONView(body).ForEach(func(key, value gjson.Result) bool {
		switch key.Str {
		case "model":
			imageIntent = imageIntent || isOpenAIImageGenerationModel(strings.TrimSpace(value.String()))
		case "tools":
			imageIntent = imageIntent || openAIJSONToolsContainNativeImageGeneration(value)
		case "input":
			imageIntent = imageIntent || openAIJSONInputContainsNativeImageGeneration(value)
		case "tool_choice":
			imageIntent = imageIntent || openAIJSONToolChoiceSelectsExplicitImageGeneration(value)
		}
		return !imageIntent
	})
	return imageIntent
}

// IsImageGenerationIntentForPlatform applies platform-specific intent rules.
//
// Codex advertises the image_gen namespace on ordinary Responses requests so
// that it is available if the model needs it. Grok strips namespace and
// Responses Lite additional_tools declarations before forwarding, so those
// declarations alone must not turn every Codex request into an image request.
// Native image_generation tools, explicit image selection and image models
// remain image intent. Other platforms retain the original declaration rule.
func IsImageGenerationIntentForPlatform(endpoint string, requestedModel string, body []byte, platform string) bool {
	if !strings.EqualFold(strings.TrimSpace(platform), PlatformGrok) {
		return IsImageGenerationIntent(endpoint, requestedModel, body)
	}
	return isExplicitGrokImageGenerationIntent(endpoint, requestedModel, body)
}

func isExplicitGrokImageGenerationIntent(endpoint string, requestedModel string, body []byte) bool {
	if IsImageGenerationEndpoint(endpoint) || isOpenAIImageGenerationModel(requestedModel) {
		return true
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}

	imageIntent := false
	parseRawJSONView(body).ForEach(func(key, value gjson.Result) bool {
		switch key.Str {
		case "model":
			imageIntent = imageIntent || isOpenAIImageGenerationModel(strings.TrimSpace(value.String()))
		case "tools":
			// Grok removes namespace catalogs before forwarding. Native
			// image_generation remains an explicit capability request.
			imageIntent = imageIntent || openAIJSONToolsContainNativeImageGeneration(value)
		case "input":
			imageIntent = imageIntent || openAIJSONInputContainsNativeImageGeneration(value)
		case "tool_choice":
			imageIntent = imageIntent || openAIJSONToolChoiceSelectsExplicitImageGeneration(value)
		}
		return !imageIntent
	})
	return imageIntent
}

// IsImageGenerationIntentMap is the map-backed variant used after service-side request mutation.
func IsImageGenerationIntentMap(endpoint string, requestedModel string, reqBody map[string]any) bool {
	if IsImageGenerationEndpoint(endpoint) {
		return true
	}
	if isOpenAIImageGenerationModel(requestedModel) {
		return true
	}
	if reqBody == nil {
		return false
	}
	if isOpenAIImageGenerationModel(firstNonEmptyString(reqBody["model"])) {
		return true
	}
	if hasOpenAIImageGenerationTool(reqBody) {
		return true
	}
	return openAIAnyToolChoiceSelectsImageGeneration(reqBody["tool_choice"])
}

// IsImageGenerationEndpoint identifies dedicated generated-image endpoints.
func IsImageGenerationEndpoint(endpoint string) bool {
	switch normalizeImageGenerationEndpoint(endpoint) {
	case "/v1/images/generations", "/v1/images/edits", "/images/generations", "/images/edits":
		return true
	default:
		return false
	}
}

func normalizeImageGenerationEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(strings.ToLower(endpoint))
	if endpoint == "" {
		return ""
	}
	endpoint = strings.TrimPrefix(endpoint, "https://api.openai.com")
	if idx := strings.IndexByte(endpoint, '?'); idx >= 0 {
		endpoint = endpoint[:idx]
	}
	return strings.TrimRight(endpoint, "/")
}

func openAIJSONToolsContainImageGeneration(tools gjson.Result) bool {
	if !tools.IsArray() {
		return false
	}
	found := false
	tools.ForEach(func(_, item gjson.Result) bool {
		if isOpenAIImageGenerationType(openAIJSONString(item.Get("type"))) {
			found = true
			return false
		}
		if isImageGenNamespaceTool(item) {
			found = true
			return false
		}
		return true
	})
	return found
}

func openAIJSONToolsContainNativeImageGeneration(tools gjson.Result) bool {
	if !tools.IsArray() {
		return false
	}
	found := false
	tools.ForEach(func(_, item gjson.Result) bool {
		found = isOpenAIImageGenerationType(openAIJSONString(item.Get("type")))
		return !found
	})
	return found
}

func isOpenAIImageGenerationType(value string) bool {
	return strings.TrimSpace(value) == "image_generation"
}

func isOpenAIImageGenNamespaceName(value string) bool {
	return strings.TrimSpace(value) == "image_gen"
}

// isImageGenNamespaceTool detects the namespace advertised by Codex's built-in
// image-generation extension instead of a hosted image_generation tool.
func isImageGenNamespaceTool(tool gjson.Result) bool {
	return openAIJSONString(tool.Get("type")) == "namespace" &&
		isOpenAIImageGenNamespaceName(openAIJSONString(tool.Get("name")))
}

// openAIJSONInputContainsImageGenTool scans Responses input items for
// additional_tools entries that declare the image_gen namespace. This covers
// the "Responses Lite" format where tools are embedded inside input items
// rather than top-level tools.
func openAIJSONInputContainsImageGenTool(input gjson.Result) bool {
	if !input.IsArray() {
		return false
	}
	found := false
	input.ForEach(func(_, item gjson.Result) bool {
		if openAIJSONString(item.Get("type")) != "additional_tools" {
			return true
		}
		found = openAIJSONToolsContainImageGeneration(item.Get("tools"))
		return !found
	})
	return found
}

func openAIJSONInputContainsNativeImageGeneration(input gjson.Result) bool {
	if !input.IsArray() {
		return false
	}
	found := false
	input.ForEach(func(_, item gjson.Result) bool {
		if openAIJSONString(item.Get("type")) != "additional_tools" {
			return true
		}
		found = openAIJSONToolsContainNativeImageGeneration(item.Get("tools"))
		return !found
	})
	return found
}

func openAIRequestBodyHasImageGenerationDeclaration(body []byte) bool {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	return openAIJSONToolsContainImageGeneration(gjson.GetBytes(body, "tools")) ||
		openAIJSONInputContainsImageGenTool(gjson.GetBytes(body, "input")) ||
		openAIJSONToolChoiceSelectsImageGeneration(gjson.GetBytes(body, "tool_choice"))
}

func openAIRequestBodyImageGenerationToolNeedsNormalization(body []byte) bool {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() {
		return false
	}
	needsNormalization := false
	tools.ForEach(func(_, item gjson.Result) bool {
		if openAIJSONString(item.Get("type")) != "image_generation" {
			return true
		}
		// 只有旧字段需要迁移时才进入 map 修改，纯计费读取保持 raw 路径。
		if item.Get("format").Exists() || item.Get("compression").Exists() {
			needsNormalization = true
			return false
		}
		return true
	})
	return needsNormalization
}

func openAIJSONToolChoiceSelectsImageGeneration(choice gjson.Result) bool {
	if !choice.Exists() {
		return false
	}
	if choice.Type == gjson.String {
		return isOpenAIImageGenerationType(choice.String())
	}
	if !choice.IsObject() {
		return false
	}
	choiceType := openAIJSONString(choice.Get("type"))
	if isOpenAIImageGenerationType(choiceType) {
		return true
	}
	if choiceType == "namespace" &&
		(isOpenAIImageGenNamespaceName(openAIJSONString(choice.Get("name"))) ||
			isOpenAIImageGenNamespaceName(openAIJSONString(choice.Get("namespace")))) {
		return true
	}
	if tool := choice.Get("tool"); tool.IsObject() && openAIJSONToolChoiceSelectsImageGeneration(tool) {
		return true
	}
	if isOpenAIImageGenerationType(openAIJSONString(choice.Get("function.name"))) {
		return true
	}
	return false
}

func openAIJSONToolChoiceSelectsExplicitImageGeneration(choice gjson.Result) bool {
	if openAIJSONToolChoiceSelectsImageGeneration(choice) {
		return true
	}
	if !choice.IsObject() {
		return false
	}
	if tool := choice.Get("tool"); tool.IsObject() && openAIJSONToolChoiceSelectsExplicitImageGeneration(tool) {
		return true
	}
	if isOpenAIImageGenFunctionReference(
		openAIJSONString(choice.Get("namespace")),
		openAIJSONString(choice.Get("name")),
	) {
		return true
	}
	if fn := choice.Get("function"); fn.IsObject() {
		return isOpenAIImageGenFunctionReference(
			openAIJSONString(fn.Get("namespace")),
			openAIJSONString(fn.Get("name")),
		)
	}
	return false
}

func isOpenAIImageGenFunctionReference(namespace string, name string) bool {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "image_gen" && name == "imagegen" {
		return true
	}
	switch name {
	case "image_gen.imagegen", "image_gen__imagegen":
		return true
	default:
		return false
	}
}

func openAIAnyToolChoiceSelectsImageGeneration(choice any) bool {
	switch v := choice.(type) {
	case string:
		return isOpenAIImageGenerationType(v)
	case map[string]any:
		choiceType := strings.TrimSpace(firstNonEmptyString(v["type"]))
		if isOpenAIImageGenerationType(choiceType) {
			return true
		}
		if choiceType == "namespace" &&
			(isOpenAIImageGenNamespaceName(firstNonEmptyString(v["name"])) ||
				isOpenAIImageGenNamespaceName(firstNonEmptyString(v["namespace"]))) {
			return true
		}
		if tool, ok := v["tool"].(map[string]any); ok && openAIAnyToolChoiceSelectsImageGeneration(tool) {
			return true
		}
		if fn, ok := v["function"].(map[string]any); ok && isOpenAIImageGenerationType(firstNonEmptyString(fn["name"])) {
			return true
		}
	}
	return false
}

func getAPIKeyFromContext(c interface{ Get(string) (any, bool) }) *APIKey {
	if c == nil {
		return nil
	}
	v, exists := c.Get("api_key")
	if !exists {
		return nil
	}
	apiKey, _ := v.(*APIKey)
	return apiKey
}

func apiKeyGroup(apiKey *APIKey) *Group {
	if apiKey == nil {
		return nil
	}
	return apiKey.Group
}

type OpenAIResponsesImageBillingConfig struct {
	Model     string
	SizeTier  string
	InputSize string
	// NativeTool is true only when the request contains exactly one native
	// image_generation tool. In that case Model/SizeTier are the tool's own
	// settlement identity rather than a top-level text-model fallback.
	NativeTool bool
}

func resolveOpenAIResponsesImageBillingConfigDetailed(reqBody map[string]any, fallbackModel string) (OpenAIResponsesImageBillingConfig, error) {
	imageModel := ""
	imageSize := ""
	imageToolCount := 0
	var imageValueErr error
	if reqBody != nil {
		forEachOpenAIResponsesNativeImageToolMap(reqBody, func(toolMap map[string]any) bool {
			imageToolCount++
			if imageToolCount > 1 {
				return false
			}
			if rawModel, exists := toolMap["model"]; exists {
				model, ok := rawModel.(string)
				if !ok {
					imageValueErr = newOpenAIResponsesNestedBillingValueError("tools[].model", "must be a string")
					return false
				}
				imageModel = strings.TrimSpace(model)
				if imageModel == "" {
					imageValueErr = newOpenAIResponsesNestedBillingValueError("tools[].model", "must not be empty")
					return false
				}
			}
			if rawSize, exists := toolMap["size"]; exists {
				size, ok := rawSize.(string)
				if !ok {
					imageValueErr = newOpenAIResponsesNestedBillingValueError("tools[].size", "must be a string")
					return false
				}
				imageSize = strings.TrimSpace(size)
			}
			return true
		})
		if imageValueErr != nil {
			return OpenAIResponsesImageBillingConfig{}, imageValueErr
		}
		if err := validateOpenAIResponsesImageToolCount(imageToolCount); err != nil {
			return OpenAIResponsesImageBillingConfig{}, err
		}
		if imageSize == "" {
			imageSize = strings.TrimSpace(firstNonEmptyString(reqBody["size"]))
		}
	}
	if imageModel == "" && reqBody != nil {
		bodyModel := strings.TrimSpace(firstNonEmptyString(reqBody["model"]))
		if isOpenAIImageBillingModelAlias(bodyModel) || imageToolCount == 0 {
			imageModel = bodyModel
		}
	}
	if imageModel == "" && imageToolCount == 1 {
		imageModel = "gpt-image-2"
	}
	if imageModel == "" {
		imageModel = strings.TrimSpace(fallbackModel)
	}
	if strings.ContainsRune(imageModel, '\x00') {
		return OpenAIResponsesImageBillingConfig{}, newOpenAIResponsesNestedBillingValueError("tools[].model", "must not contain null characters")
	}
	if err := validateOpenAIResponsesBillingSizeString("size", imageSize); err != nil {
		return OpenAIResponsesImageBillingConfig{}, err
	}
	sizeTier := normalizeOpenAIImageSizeTier(imageSize)
	return OpenAIResponsesImageBillingConfig{
		Model:      imageModel,
		SizeTier:   sizeTier,
		InputSize:  imageSize,
		NativeTool: imageToolCount == 1,
	}, nil
}

func forEachOpenAIResponsesNativeImageToolMap(reqBody map[string]any, visit func(map[string]any) bool) {
	if reqBody == nil || visit == nil {
		return
	}
	if !forEachOpenAIResponsesNativeImageToolListMap(reqBody["tools"], visit) {
		return
	}
	input, _ := reqBody["input"].([]any)
	for _, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if !ok || strings.TrimSpace(firstNonEmptyString(item["type"])) != "additional_tools" {
			continue
		}
		if !forEachOpenAIResponsesNativeImageToolListMap(item["tools"], visit) {
			return
		}
	}
}

func forEachOpenAIResponsesNativeImageToolListMap(rawTools any, visit func(map[string]any) bool) bool {
	tools, _ := rawTools.([]any)
	for _, rawTool := range tools {
		toolMap, ok := rawTool.(map[string]any)
		if !ok || strings.TrimSpace(firstNonEmptyString(toolMap["type"])) != "image_generation" {
			continue
		}
		if !visit(toolMap) {
			return false
		}
	}
	return true
}

func resolveOpenAIResponsesImageBillingConfigFromBody(body []byte, fallbackModel string) (string, string, error) {
	cfg, err := resolveOpenAIResponsesImageBillingConfigDetailedFromBody(body, fallbackModel)
	if err != nil {
		return "", "", err
	}
	return cfg.Model, cfg.SizeTier, nil
}

func resolveOpenAIResponsesImageBillingConfigDetailedFromBody(body []byte, fallbackModel string) (OpenAIResponsesImageBillingConfig, error) {
	imageModel := ""
	imageSize := ""
	imageToolCount := 0
	if len(body) > 0 && gjson.ValidBytes(body) {
		if err := ValidateUniqueOpenAIResponsesBillingFields(body); err != nil {
			return OpenAIResponsesImageBillingConfig{}, err
		}
		tools := gjson.GetBytes(body, "tools")
		input := gjson.GetBytes(body, "input")
		bodyModel := openAIJSONString(gjson.GetBytes(body, "model"))
		bodySize := openAIJSONString(gjson.GetBytes(body, "size"))
		forEachOpenAIResponsesNativeImageToolJSON(tools, input, func(item gjson.Result) bool {
			imageToolCount++
			if imageToolCount > 1 {
				return false
			}
			imageModel = openAIJSONString(item.Get("model"))
			imageSize = openAIJSONString(item.Get("size"))
			return true
		})
		if err := validateOpenAIResponsesImageToolCount(imageToolCount); err != nil {
			return OpenAIResponsesImageBillingConfig{}, err
		}
		if imageSize == "" {
			imageSize = bodySize
		}
		if imageModel == "" {
			if isOpenAIImageBillingModelAlias(bodyModel) || imageToolCount == 0 {
				imageModel = bodyModel
			}
		}
	}
	if imageModel == "" && imageToolCount == 1 {
		imageModel = "gpt-image-2"
	}
	if imageModel == "" {
		imageModel = strings.TrimSpace(fallbackModel)
	}
	return OpenAIResponsesImageBillingConfig{
		Model:      imageModel,
		SizeTier:   normalizeOpenAIImageSizeTier(imageSize),
		InputSize:  imageSize,
		NativeTool: imageToolCount == 1,
	}, nil
}

// ResolveOpenAIResponsesImageBillingConfigFromBody exposes the validated
// Responses media identity to protocol handlers before account selection.
// Native image tools must be admitted using their own model/tier; a priced
// top-level text model is not evidence that the nested media SKU is priced.
func ResolveOpenAIResponsesImageBillingConfigFromBody(
	body []byte,
	fallbackModel string,
) (OpenAIResponsesImageBillingConfig, error) {
	return resolveOpenAIResponsesImageBillingConfigDetailedFromBody(body, fallbackModel)
}

func forEachOpenAIResponsesNativeImageToolJSON(
	tools gjson.Result,
	input gjson.Result,
	visit func(gjson.Result) bool,
) {
	if visit == nil {
		return
	}
	if !forEachOpenAIResponsesNativeImageToolListJSON(tools, visit) || !input.IsArray() {
		return
	}
	keepGoing := true
	input.ForEach(func(_, item gjson.Result) bool {
		if openAIJSONString(item.Get("type")) != "additional_tools" {
			return true
		}
		keepGoing = forEachOpenAIResponsesNativeImageToolListJSON(item.Get("tools"), visit)
		return keepGoing
	})
}

func forEachOpenAIResponsesNativeImageToolListJSON(tools gjson.Result, visit func(gjson.Result) bool) bool {
	if !tools.IsArray() {
		return true
	}
	keepGoing := true
	tools.ForEach(func(_, item gjson.Result) bool {
		if openAIJSONString(item.Get("type")) != "image_generation" {
			return true
		}
		keepGoing = visit(item)
		return keepGoing
	})
	return keepGoing
}

// ValidateUniqueOpenAIResponsesBillingFields rejects ambiguous Responses JSON
// before intent detection, account selection, or forwarding. JSON consumers
// disagree on whether the first or last duplicate key wins; every field that
// can change media routing or settlement must therefore have one identity.
func ValidateUniqueOpenAIResponsesBillingFields(body []byte) error {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return nil
	}

	root := parseRawJSONView(body)
	relevantFieldCounts := make(map[string]int, 6)
	var model, size, tools, input, toolChoice gjson.Result
	root.ForEach(func(key, value gjson.Result) bool {
		switch key.Str {
		case "model":
			relevantFieldCounts[key.Str]++
			if relevantFieldCounts[key.Str] == 1 {
				model = value
			}
		case "tools":
			relevantFieldCounts[key.Str]++
			if relevantFieldCounts[key.Str] == 1 {
				tools = value
			}
		case "size":
			relevantFieldCounts[key.Str]++
			if relevantFieldCounts[key.Str] == 1 {
				size = value
			}
		case "service_tier":
			relevantFieldCounts[key.Str]++
		case "input":
			relevantFieldCounts[key.Str]++
			if relevantFieldCounts[key.Str] == 1 {
				input = value
			}
		case "tool_choice":
			relevantFieldCounts[key.Str]++
			if relevantFieldCounts[key.Str] == 1 {
				toolChoice = value
			}
		}
		return true
	})
	for _, field := range []string{"model", "tools", "size", "input", "tool_choice", "service_tier"} {
		if err := validateUniqueOpenAIResponsesBillingField(field, relevantFieldCounts[field]); err != nil {
			return err
		}
	}
	if relevantFieldCounts["model"] == 1 {
		if err := validateOpenAIResponsesBillingModelResult("model", model); err != nil {
			return err
		}
	}
	if relevantFieldCounts["size"] == 1 {
		if err := validateOpenAIResponsesBillingSizeResult("size", size); err != nil {
			return err
		}
	}
	if err := validateOpenAIResponsesToolArrayBillingFields(tools, "tools"); err != nil {
		return err
	}
	if err := validateOpenAIResponsesInputToolBillingFields(input); err != nil {
		return err
	}
	return validateOpenAIResponsesToolChoiceBillingFields(toolChoice, "tool_choice")
}

func validateOpenAIResponsesToolArrayBillingFields(tools gjson.Result, path string) error {
	if !tools.IsArray() {
		return nil
	}
	var validationErr error
	index := 0
	tools.ForEach(func(_, item gjson.Result) bool {
		itemPath := fmt.Sprintf("%s[%d]", path, index)
		index++
		if !item.IsObject() {
			return true
		}

		fieldCounts := make(map[string]int, 6)
		var model, size, nestedTools, nestedFunction gjson.Result
		item.ForEach(func(key, value gjson.Result) bool {
			switch key.Str {
			case "type", "name", "namespace":
				fieldCounts[key.Str]++
			case "model":
				fieldCounts[key.Str]++
				if fieldCounts[key.Str] == 1 {
					model = value
				}
			case "size":
				fieldCounts[key.Str]++
				if fieldCounts[key.Str] == 1 {
					size = value
				}
			case "tools":
				fieldCounts[key.Str]++
				if fieldCounts[key.Str] == 1 {
					nestedTools = value
				}
			case "function":
				fieldCounts[key.Str]++
				if fieldCounts[key.Str] == 1 {
					nestedFunction = value
				}
			}
			return true
		})
		for _, field := range []string{"type", "model", "size", "name", "namespace", "tools", "function"} {
			if fieldCounts[field] > 1 {
				validationErr = newOpenAIResponsesNestedBillingFieldError(itemPath + "." + field)
				return false
			}
		}
		if fieldCounts["model"] == 1 {
			if err := validateOpenAIResponsesBillingModelResult(itemPath+".model", model); err != nil {
				validationErr = err
				return false
			}
		}
		if fieldCounts["size"] == 1 {
			if err := validateOpenAIResponsesBillingSizeResult(itemPath+".size", size); err != nil {
				validationErr = err
				return false
			}
		}
		if err := validateOpenAIResponsesToolArrayBillingFields(nestedTools, itemPath+".tools"); err != nil {
			validationErr = err
			return false
		}
		if err := validateOpenAIResponsesToolChoiceBillingFields(nestedFunction, itemPath+".function"); err != nil {
			validationErr = err
			return false
		}
		return true
	})
	return validationErr
}

func validateOpenAIResponsesInputToolBillingFields(input gjson.Result) error {
	if !input.IsArray() {
		return nil
	}
	var validationErr error
	index := 0
	input.ForEach(func(_, item gjson.Result) bool {
		itemPath := fmt.Sprintf("input[%d]", index)
		index++
		if !item.IsObject() {
			return true
		}
		typeCount := 0
		toolsCount := 0
		var nestedTools gjson.Result
		item.ForEach(func(key, value gjson.Result) bool {
			switch key.Str {
			case "type":
				typeCount++
			case "tools":
				toolsCount++
				if toolsCount == 1 {
					nestedTools = value
				}
			}
			return true
		})
		if toolsCount == 0 {
			return true
		}
		if typeCount > 1 {
			validationErr = newOpenAIResponsesNestedBillingFieldError(itemPath + ".type")
			return false
		}
		if toolsCount > 1 {
			validationErr = newOpenAIResponsesNestedBillingFieldError(itemPath + ".tools")
			return false
		}
		if err := validateOpenAIResponsesToolArrayBillingFields(nestedTools, itemPath+".tools"); err != nil {
			validationErr = err
			return false
		}
		return true
	})
	return validationErr
}

func validateOpenAIResponsesToolChoiceBillingFields(choice gjson.Result, path string) error {
	if !choice.IsObject() {
		return nil
	}
	fieldCounts := make(map[string]int, 7)
	var model, size, nestedTool, nestedFunction gjson.Result
	choice.ForEach(func(key, value gjson.Result) bool {
		switch key.Str {
		case "type", "name", "namespace":
			fieldCounts[key.Str]++
		case "model":
			fieldCounts[key.Str]++
			if fieldCounts[key.Str] == 1 {
				model = value
			}
		case "size":
			fieldCounts[key.Str]++
			if fieldCounts[key.Str] == 1 {
				size = value
			}
		case "tool":
			fieldCounts[key.Str]++
			if fieldCounts[key.Str] == 1 {
				nestedTool = value
			}
		case "function":
			fieldCounts[key.Str]++
			if fieldCounts[key.Str] == 1 {
				nestedFunction = value
			}
		}
		return true
	})
	for _, field := range []string{"type", "model", "size", "name", "namespace", "tool", "function"} {
		if fieldCounts[field] > 1 {
			return newOpenAIResponsesNestedBillingFieldError(path + "." + field)
		}
	}
	if fieldCounts["model"] == 1 {
		if err := validateOpenAIResponsesBillingModelResult(path+".model", model); err != nil {
			return err
		}
	}
	if fieldCounts["size"] == 1 {
		if err := validateOpenAIResponsesBillingSizeResult(path+".size", size); err != nil {
			return err
		}
	}
	if err := validateOpenAIResponsesToolChoiceBillingFields(nestedTool, path+".tool"); err != nil {
		return err
	}
	return validateOpenAIResponsesToolChoiceBillingFields(nestedFunction, path+".function")
}

func newOpenAIResponsesNestedBillingFieldError(path string) error {
	return fmt.Errorf(
		"%w: duplicate %q fields make the Responses billing identity ambiguous",
		ErrModelPricingUnavailable,
		path,
	)
}

func newOpenAIResponsesNestedBillingValueError(path string, message string) error {
	return fmt.Errorf(
		"%w: %q %s",
		ErrModelPricingUnavailable,
		path,
		message,
	)
}

func validateOpenAIResponsesBillingModelResult(path string, value gjson.Result) error {
	if value.Type != gjson.String {
		return newOpenAIResponsesNestedBillingValueError(path, "must be a string")
	}
	model := strings.TrimSpace(value.String())
	if model == "" {
		return newOpenAIResponsesNestedBillingValueError(path, "must not be empty")
	}
	if strings.ContainsRune(model, '\x00') {
		return newOpenAIResponsesNestedBillingValueError(path, "must not contain null characters")
	}
	return nil
}

func validateOpenAIResponsesBillingSizeResult(path string, value gjson.Result) error {
	if value.Type != gjson.String {
		return newOpenAIResponsesNestedBillingValueError(path, "must be a string")
	}
	return validateOpenAIResponsesBillingSizeString(path, value.String())
}

func validateOpenAIResponsesBillingSizeString(path string, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "auto") {
		return nil
	}
	if strings.ContainsRune(value, '\x00') {
		return newOpenAIResponsesNestedBillingValueError(path, "must not contain null characters")
	}
	if _, ok := ClassifyImageBillingTier(value); !ok {
		return newOpenAIResponsesNestedBillingValueError(path, "has no configured image billing tier")
	}
	return nil
}

func validateUniqueOpenAIResponsesBillingField(field string, count int) error {
	if count <= 1 {
		return nil
	}
	return fmt.Errorf(
		"%w: duplicate top-level %q fields make the image billing identity ambiguous",
		ErrModelPricingUnavailable,
		field,
	)
}

func validateOpenAIResponsesImageToolCount(count int) error {
	if count <= 1 {
		return nil
	}
	return fmt.Errorf(
		"%w: multiple image_generation tools make the billing model and size ambiguous",
		ErrModelPricingUnavailable,
	)
}

func isOpenAIImageBillingModelAlias(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return false
	}
	return isOpenAIImageGenerationModel(normalized) || strings.Contains(normalized, "image")
}

func openAIJSONString(value gjson.Result) string {
	if value.Type != gjson.String {
		return ""
	}
	return strings.TrimSpace(value.String())
}
