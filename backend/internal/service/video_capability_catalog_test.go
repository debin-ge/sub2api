package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type videoCapabilitySettingStub struct {
	SettingRepository
	value string
	err   error
	sets  int
}

func (s *videoCapabilitySettingStub) GetValue(context.Context, string) (string, error) {
	return s.value, s.err
}

func (s *videoCapabilitySettingStub) Set(_ context.Context, _ string, value string) error {
	if s.err != nil {
		return s.err
	}
	s.value = value
	s.sets++
	return nil
}

func TestDefaultVideoCapabilityCatalogIsValid(t *testing.T) {
	document := DefaultVideoCapabilityCatalogDocument()
	require.NoError(t, ValidateVideoCapabilityCatalog(document))
	require.Equal(t, OpenAIVideoModelSora2, document.Providers[VideoProviderOpenAI].DefaultModel)
}

func TestVideoCapabilityCatalogRejectsUnsafeProviderName(t *testing.T) {
	document := DefaultVideoCapabilityCatalogDocument()
	document.Providers["OpenAI unsafe"] = DefaultOpenAIVideoCapabilities()

	require.ErrorIs(t, ValidateVideoCapabilityCatalog(document), ErrVideoInvalidRequest)
}

func TestVideoCapabilityCatalogRefreshKeepsLastKnownGood(t *testing.T) {
	repository := &videoCapabilitySettingStub{}
	catalog := NewVideoCapabilityCatalog(repository)
	document := DefaultVideoCapabilityCatalogDocument()
	capabilities := document.Providers[VideoProviderOpenAI]
	capabilities.DefaultSeconds[OpenAIVideoModelSora2] = 8
	document.Providers[VideoProviderOpenAI] = capabilities
	raw, err := json.Marshal(document)
	require.NoError(t, err)
	repository.value = string(raw)

	require.NoError(t, catalog.Refresh(context.Background()))
	loaded, ok := catalog.Capabilities(VideoProviderOpenAI)
	require.True(t, ok)
	require.Equal(t, 8, loaded.DefaultSeconds[OpenAIVideoModelSora2])

	repository.value = `{"version":1,"providers":{"openai":{"unknown":true}}}`
	require.Error(t, catalog.Refresh(context.Background()))
	loaded, ok = catalog.Capabilities(VideoProviderOpenAI)
	require.True(t, ok)
	require.Equal(t, 8, loaded.DefaultSeconds[OpenAIVideoModelSora2])
	require.NotEmpty(t, catalog.View().LastRefreshError)
}

func TestVideoCapabilityCatalogUpdateValidatesBeforePersisting(t *testing.T) {
	repository := &videoCapabilitySettingStub{}
	catalog := NewVideoCapabilityCatalog(repository)
	invalid := DefaultVideoCapabilityCatalogDocument()
	capabilities := invalid.Providers[VideoProviderOpenAI]
	capabilities.DefaultSeconds[OpenAIVideoModelSora2] = 999
	invalid.Providers[VideoProviderOpenAI] = capabilities

	_, err := catalog.Update(context.Background(), invalid)

	require.Error(t, err)
	require.Zero(t, repository.sets)
	require.Equal(t, 4, catalog.View().Providers[VideoProviderOpenAI].DefaultSeconds[OpenAIVideoModelSora2])
}

func TestDecodeVideoCapabilityCatalogRejectsDuplicateAndTrailingJSON(t *testing.T) {
	_, err := DecodeVideoCapabilityCatalog([]byte(`{"version":1,"version":1,"providers":{}}`))
	require.Error(t, err)
	_, err = DecodeVideoCapabilityCatalog([]byte(`{"version":1,"providers":{}} {}`))
	require.Error(t, err)
}

func TestVideoCapabilityCatalogRefreshFailureDoesNotDiscardBuiltin(t *testing.T) {
	repository := &videoCapabilitySettingStub{err: errors.New("database unavailable")}
	catalog := NewVideoCapabilityCatalog(repository)
	catalog.ttl = time.Hour

	require.Error(t, catalog.Refresh(context.Background()))
	capabilities, ok := catalog.Capabilities(VideoProviderOpenAI)
	require.True(t, ok)
	require.Equal(t, OpenAIVideoModelSora2, capabilities.DefaultModel)
}

func TestVideoCapabilityCatalogRejectsUnknownDisabledCapabilityAndOrphanSpecs(t *testing.T) {
	document := DefaultVideoCapabilityCatalogDocument()
	capabilities := document.Providers[VideoProviderOpenAI]
	capabilities.Operations[VideoCapability("typo")] = false
	document.Providers[VideoProviderOpenAI] = capabilities
	require.Error(t, ValidateVideoCapabilityCatalog(document))

	document = DefaultVideoCapabilityCatalogDocument()
	capabilities = document.Providers[VideoProviderOpenAI]
	capabilities.SupportedSeconds["sora-unknown"] = []int{4}
	document.Providers[VideoProviderOpenAI] = capabilities
	require.Error(t, ValidateVideoCapabilityCatalog(document))
}
