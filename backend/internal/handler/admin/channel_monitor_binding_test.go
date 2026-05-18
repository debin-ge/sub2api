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
			for _, provider := range []string{"minimax", "glm", "kimi", "deepseek", "windsurf", "opencode"} {
				if !strings.Contains(binding, provider) {
					t.Fatalf("binding %q does not include provider %q", binding, provider)
				}
			}
		})
	}
}
