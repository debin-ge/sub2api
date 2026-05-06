package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeGLMModelIDsUsesConfiguredModelsWhenMappingsExist(t *testing.T) {
	got := mergeGLMModelIDs([]string{
		"GLM-4.7",
		"custom-glm-alias",
		"claude-sonnet-*",
		"",
	})

	require.Equal(t, []string{
		"GLM-4.7",
		"custom-glm-alias",
	}, got)
}

func TestMergeGLMModelIDsFallsBackToOfficialModels(t *testing.T) {
	got := mergeGLMModelIDs(nil)

	require.Equal(t, []string{
		"GLM-5.1",
		"GLM-4.7",
		"GLM-4.5-air",
	}, got)
}
