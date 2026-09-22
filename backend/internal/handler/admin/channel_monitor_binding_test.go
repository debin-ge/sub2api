package admin

import (
	"reflect"
	"strings"
	"testing"
)

func TestChannelMonitorProviderBindingsAllowDomesticProviders(t *testing.T) {
	cases := []struct {
		name  string
		model any
		field string
	}{
		{name: "monitor create", model: channelMonitorCreateRequest{}, field: "Provider"},
		{name: "monitor update", model: channelMonitorUpdateRequest{}, field: "Provider"},
		{name: "template create", model: channelMonitorTemplateCreateRequest{}, field: "Provider"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field, ok := reflect.TypeOf(tc.model).FieldByName(tc.field)
			if !ok {
				t.Fatalf("field %s not found", tc.field)
			}
			binding := field.Tag.Get("binding")
			for _, provider := range []string{"minimax", "kimi", "deepseek", "windsurf", "opencode"} {
				if !strings.Contains(binding, provider) {
					t.Fatalf("binding %q does not include provider %q", binding, provider)
				}
			}
		})
	}
}

// glm 是 zhipu 的历史平台 ID：新建监控禁止使用（与账号/分组一致），但编辑历史
// 监控、以及监控模板（本身不直接创建监控行）必须继续允许 glm。
func TestChannelMonitorProviderBindingsGLMCreateVsUpdate(t *testing.T) {
	createField, ok := reflect.TypeOf(channelMonitorCreateRequest{}).FieldByName("Provider")
	if !ok {
		t.Fatal("field Provider not found on channelMonitorCreateRequest")
	}
	if strings.Contains(createField.Tag.Get("binding"), "glm") {
		t.Fatalf("channelMonitorCreateRequest.Provider binding %q 不应再包含 glm（禁止新建）", createField.Tag.Get("binding"))
	}

	updateField, ok := reflect.TypeOf(channelMonitorUpdateRequest{}).FieldByName("Provider")
	if !ok {
		t.Fatal("field Provider not found on channelMonitorUpdateRequest")
	}
	if !strings.Contains(updateField.Tag.Get("binding"), "glm") {
		t.Fatalf("channelMonitorUpdateRequest.Provider binding %q 应保留 glm（编辑历史监控）", updateField.Tag.Get("binding"))
	}

	templateField, ok := reflect.TypeOf(channelMonitorTemplateCreateRequest{}).FieldByName("Provider")
	if !ok {
		t.Fatal("field Provider not found on channelMonitorTemplateCreateRequest")
	}
	if !strings.Contains(templateField.Tag.Get("binding"), "glm") {
		t.Fatalf("channelMonitorTemplateCreateRequest.Provider binding %q 应保留 glm（模板不直接创建监控行，实际创建仍受 channelMonitorCreateRequest 拦截）", templateField.Tag.Get("binding"))
	}
}
