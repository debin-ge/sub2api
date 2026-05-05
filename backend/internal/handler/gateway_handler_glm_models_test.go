package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeGLMModelIDsKeepsOfficialModelsWhenMappingsExist(t *testing.T) {
	got := mergeGLMModelIDs([]string{
		"claude-sonnet-4-5",
		"GLM-5.1",
		"custom-glm-alias",
		"",
	})

	require.Equal(t, []string{
		"GLM-5.1",
		"GLM-4.7",
		"GLM-4.5-air",
		"claude-sonnet-4-5",
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
