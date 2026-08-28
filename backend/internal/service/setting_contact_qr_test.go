//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

const serviceTestContactQRCodePNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestNormalizeContactQRCodeValidatesDecodedImageContent(t *testing.T) {
	value, err := normalizeContactQRCode(serviceTestContactQRCodePNG)
	require.NoError(t, err)
	require.Equal(t, serviceTestContactQRCodePNG, value)

	_, err = normalizeContactQRCode("data:image/png;base64,iVBORw0KGgo=")
	require.Error(t, err, "a PNG signature without a decodable image must be rejected")

	_, err = normalizeContactQRCode("data:image/jpeg;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	require.Error(t, err, "the declared MIME type must match the decoded image")
}

func TestSettingService_GetContactQRCodeImageBuildsStableRevision(t *testing.T) {
	svc := NewSettingService(&settingPublicRepoStub{values: map[string]string{
		SettingKeyContactQRCode: serviceTestContactQRCodePNG,
	}}, &config.Config{})

	first, err := svc.GetContactQRCodeImage(context.Background())
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, "image/png", first.ContentType)
	require.Len(t, first.Revision, 64)
	require.Equal(t, `"`+first.Revision+`"`, first.ETag)
	require.NotEmpty(t, first.Data)

	second, err := svc.GetContactQRCodeImage(context.Background())
	require.NoError(t, err)
	require.Equal(t, first.Revision, second.Revision)
	require.Equal(t, first.ETag, second.ETag)
}

func TestSettingService_GetContactQRCodeImageTreatsMissingSettingAsDisabled(t *testing.T) {
	svc := NewSettingService(&settingPublicRepoStub{
		values: map[string]string{},
		err:    ErrSettingNotFound,
	}, &config.Config{})

	image, err := svc.GetContactQRCodeImage(context.Background())
	require.NoError(t, err)
	require.Nil(t, image)
}
