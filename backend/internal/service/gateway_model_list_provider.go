package service

import (
	"sort"
	"strings"
)

type GatewayModelListOptions struct {
	IncludeAliases bool
}

type GatewayModelListProvider struct {
	options GatewayModelListOptions
}

func NewGatewayModelListProvider(options GatewayModelListOptions) *GatewayModelListProvider {
	return &GatewayModelListProvider{options: options}
}

func (p *GatewayModelListProvider) ModelsForProvider(platform string, accounts []Account) []string {
	caps, ok := domesticProviderCapabilities[platform]
	if !ok {
		return nil
	}

	modelSet := make(map[string]struct{}, len(caps.PublicModelIDs)+len(accounts))
	for _, model := range caps.PublicModelIDs {
		modelSet[model] = struct{}{}
	}
	if p != nil && p.options.IncludeAliases {
		for _, rule := range caps.AliasRules {
			if !strings.Contains(rule.AliasPattern, "*") {
				modelSet[rule.AliasPattern] = struct{}{}
			}
		}
	}

	for i := range accounts {
		acc := accounts[i]
		if acc.Platform != platform {
			continue
		}
		for requested, upstream := range acc.GetModelMapping() {
			requested = strings.TrimSpace(requested)
			upstream = strings.TrimSpace(upstream)
			if requested == "" || strings.Contains(requested, "*") {
				continue
			}
			if providerSupportsUpstreamModel(platform, upstream) {
				modelSet[requested] = struct{}{}
			}
		}
	}

	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}
