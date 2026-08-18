package repository

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wenlng/go-captcha/v2/click"
	"github.com/wenlng/go-captcha/v2/rotate"
	"github.com/wenlng/go-captcha/v2/slide"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func newGoCaptchaGeneratorForTest() service.GoCaptchaGenerator {
	return NewGoCaptchaGenerator(&config.Config{
		GoCaptcha: config.GoCaptchaConfig{ClickVerifyLen: 3},
	})
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

func TestGoCaptchaGenerateProducesImagesForEveryMode(t *testing.T) {
	generator := newGoCaptchaGeneratorForTest()

	modes := []service.GoCaptchaMode{
		service.GoCaptchaModeClick,
		service.GoCaptchaModeShape,
		service.GoCaptchaModeSlide,
		service.GoCaptchaModeDrag,
		service.GoCaptchaModeRotate,
	}

	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			challenge, err := generator.Generate(mode)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(challenge.MasterImage, "data:image/"))
			require.True(t, strings.HasPrefix(challenge.ThumbImage, "data:image/"))
			require.NotEmpty(t, challenge.Answer)

			switch mode {
			case service.GoCaptchaModeSlide, service.GoCaptchaModeDrag:
				require.NotZero(t, challenge.TileWidth)
				require.NotZero(t, challenge.TileHeight)
				require.Zero(t, challenge.ThumbSize)
			case service.GoCaptchaModeRotate:
				require.NotZero(t, challenge.ThumbSize)
				require.Zero(t, challenge.TileWidth)
			default:
				// 点选不下发任何几何参数，缩略图本身就是题面
				require.Zero(t, challenge.TileWidth)
				require.Zero(t, challenge.ThumbSize)
			}
		})
	}
}

func TestGoCaptchaGenerateSlideDoesNotExposeGapPosition(t *testing.T) {
	generator := newGoCaptchaGeneratorForTest()

	challenge, err := generator.Generate(service.GoCaptchaModeSlide)
	require.NoError(t, err)

	var answer slide.Block
	require.NoError(t, json.Unmarshal(challenge.Answer, &answer))
	// 下发的是拼图块起始位置 DX/DY，缺口位置 X/Y 只能留在服务端
	require.Equal(t, answer.DX, challenge.TileX)
	require.Equal(t, answer.DY, challenge.TileY)
	require.NotEqual(t, answer.X, challenge.TileX, "gap X must not be handed to the client")
}

func TestGoCaptchaGenerateRotateDoesNotExposeAngle(t *testing.T) {
	generator := newGoCaptchaGeneratorForTest()

	challenge, err := generator.Generate(service.GoCaptchaModeRotate)
	require.NoError(t, err)

	var answer rotate.Block
	require.NoError(t, json.Unmarshal(challenge.Answer, &answer))
	require.Equal(t, answer.Width, challenge.ThumbSize)
	require.NotZero(t, answer.Angle)
	require.GreaterOrEqual(t, answer.Angle, rotateMinAngle)
	require.LessOrEqual(t, answer.Angle, rotateMaxAngle)
}

func TestGoCaptchaValidateClickRequiresCorrectOrder(t *testing.T) {
	generator := newGoCaptchaGeneratorForTest()
	answer := mustMarshal(t, map[int]*click.Dot{
		0: {X: 100, Y: 50, Width: 30, Height: 30},
		1: {X: 200, Y: 80, Width: 30, Height: 30},
	})

	require.True(t, generator.Validate(service.GoCaptchaModeClick, answer, "105,55,205,85", 5))
	// 顺序颠倒必须失败
	require.False(t, generator.Validate(service.GoCaptchaModeClick, answer, "205,85,105,55", 5))
}

func TestGoCaptchaValidateClickAppliesPaddingSymmetrically(t *testing.T) {
	generator := newGoCaptchaGeneratorForTest()
	answer := mustMarshal(t, map[int]*click.Dot{
		0: {X: 100, Y: 50, Width: 30, Height: 30},
	})

	require.True(t, generator.Validate(service.GoCaptchaModeClick, answer, "95,45", 5))
	require.True(t, generator.Validate(service.GoCaptchaModeClick, answer, "135,85", 5))
	require.False(t, generator.Validate(service.GoCaptchaModeClick, answer, "94,45", 5))
	require.False(t, generator.Validate(service.GoCaptchaModeClick, answer, "95,44", 5))
}

func TestGoCaptchaValidateClickRejectsShortSubmission(t *testing.T) {
	generator := newGoCaptchaGeneratorForTest()
	answer := mustMarshal(t, map[int]*click.Dot{
		0: {X: 100, Y: 50, Width: 30, Height: 30},
		1: {X: 200, Y: 80, Width: 30, Height: 30},
	})

	// 少提交一个点不能通过，否则点选的强度会坍缩到单点
	require.False(t, generator.Validate(service.GoCaptchaModeClick, answer, "105,55", 5))
	require.False(t, generator.Validate(service.GoCaptchaModeClick, answer, "105,55,205,85,300,90", 5))
}

func TestGoCaptchaValidateClickRejectsMalformedSubmission(t *testing.T) {
	generator := newGoCaptchaGeneratorForTest()
	answer := mustMarshal(t, map[int]*click.Dot{
		0: {X: 100, Y: 50, Width: 30, Height: 30},
	})

	require.False(t, generator.Validate(service.GoCaptchaModeClick, answer, "abc,55", 5))
	require.False(t, generator.Validate(service.GoCaptchaModeClick, answer, "", 5))
}

func TestGoCaptchaValidateShapeUsesClickRules(t *testing.T) {
	generator := newGoCaptchaGeneratorForTest()
	answer := mustMarshal(t, map[int]*click.Dot{
		0: {X: 100, Y: 50, Width: 30, Height: 30},
	})

	require.True(t, generator.Validate(service.GoCaptchaModeShape, answer, "105,55", 5))
	require.False(t, generator.Validate(service.GoCaptchaModeShape, answer, "10,10", 5))
}

func TestGoCaptchaValidateSlideRespectsPadding(t *testing.T) {
	generator := newGoCaptchaGeneratorForTest()
	answer := mustMarshal(t, slide.Block{X: 200, Y: 80})

	require.True(t, generator.Validate(service.GoCaptchaModeSlide, answer, "203,82", 5))
	require.False(t, generator.Validate(service.GoCaptchaModeSlide, answer, "220,80", 5))
	// 字段数不对直接判失败
	require.False(t, generator.Validate(service.GoCaptchaModeSlide, answer, "203", 5))
	require.False(t, generator.Validate(service.GoCaptchaModeSlide, answer, "203,82,10", 5))
}

func TestGoCaptchaValidateDragChecksBothAxes(t *testing.T) {
	generator := newGoCaptchaGeneratorForTest()
	answer := mustMarshal(t, slide.Block{X: 200, Y: 80})

	require.True(t, generator.Validate(service.GoCaptchaModeDrag, answer, "200,80", 5))
	// 拖拽的 Y 也是自由的，Y 猜错同样失败
	require.False(t, generator.Validate(service.GoCaptchaModeDrag, answer, "200,140", 5))
}

func TestGoCaptchaValidateRotate(t *testing.T) {
	generator := newGoCaptchaGeneratorForTest()
	answer := mustMarshal(t, rotate.Block{Angle: 100})

	// 用户旋转角度与目标角度之和需要凑满一圈
	require.True(t, generator.Validate(service.GoCaptchaModeRotate, answer, "260", 8))
	require.True(t, generator.Validate(service.GoCaptchaModeRotate, answer, "255", 8))
	require.False(t, generator.Validate(service.GoCaptchaModeRotate, answer, "200", 8))
	require.False(t, generator.Validate(service.GoCaptchaModeRotate, answer, "260,10", 8))
	require.False(t, generator.Validate(service.GoCaptchaModeRotate, answer, "not-a-number", 8))
}

func TestGoCaptchaValidateRejectsAnswerOfWrongShape(t *testing.T) {
	generator := newGoCaptchaGeneratorForTest()

	require.False(t, generator.Validate(service.GoCaptchaModeSlide, json.RawMessage(`"nope"`), "1,2", 5))
	require.False(t, generator.Validate(service.GoCaptchaModeRotate, json.RawMessage(`[]`), "260", 8))
	require.False(t, generator.Validate(service.GoCaptchaModeClick, json.RawMessage(`{}`), "1,2", 5))
}
