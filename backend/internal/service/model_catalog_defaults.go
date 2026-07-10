package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

func DefaultModelCatalogIDs(platform string) []string {
	platform = strings.TrimSpace(platform)
	var ids []string
	switch platform {
	case PlatformAnthropic:
		ids = claude.DefaultModelIDs()
	case PlatformOpenAI:
		ids = openai.DefaultModelIDs()
	case PlatformGemini:
		ids = make([]string, 0, len(geminicli.DefaultModels))
		for _, model := range geminicli.DefaultModels {
			ids = append(ids, model.ID)
		}
	case PlatformAntigravity:
		for _, model := range antigravity.DefaultModels() {
			ids = append(ids, model.ID)
		}
	case PlatformGrok:
		ids = xai.DefaultModelIDs()
	default:
		ids = DefaultDomesticProviderModelIDs(platform)
	}
	return cloneStringSlice(ids)
}
