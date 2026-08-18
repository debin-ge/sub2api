package repository

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/golang/freetype/truetype"
	"github.com/wenlng/go-captcha-assets/bindata/chars"
	"github.com/wenlng/go-captcha-assets/resources/fonts/fzshengsksjw"
	"github.com/wenlng/go-captcha-assets/resources/imagesv2"
	"github.com/wenlng/go-captcha-assets/resources/shapes"
	"github.com/wenlng/go-captcha-assets/resources/tiles"
	"github.com/wenlng/go-captcha/v2/base/option"
	"github.com/wenlng/go-captcha/v2/click"
	"github.com/wenlng/go-captcha/v2/rotate"
	"github.com/wenlng/go-captcha/v2/slide"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	// defaultClickVerifyLen 点选需要按顺序点中的目标数量。
	// 降到 2 会让盲猜成功率上升约两个数量级。
	defaultClickVerifyLen = 3
	// rotateMinAngle / rotateMaxAngle 避开接近 0 度的近似正确姿态
	rotateMinAngle = 20
	rotateMaxAngle = 330
)

// goCaptchaGenerator 是整个后端唯一 import go-captcha 的地方。
// 五种模式的实例在首次使用时一次性构建：素材是共享的，多建几个 builder 的
// 额外开销可以忽略，换来的是后台切换模式时无需任何重建。
type goCaptchaGenerator struct {
	cfg *config.Config

	once       sync.Once
	initErr    error
	clickCapt  click.Captcha
	shapeCapt  click.Captcha
	slideCapt  slide.Captcha
	dragCapt   slide.Captcha
	rotateCapt rotate.Captcha
}

// NewGoCaptchaGenerator 构造行为验证码生成器。
// 素材加载与实例构建是懒初始化的，未启用该 provider 的部署不承担启动开销。
func NewGoCaptchaGenerator(cfg *config.Config) service.GoCaptchaGenerator {
	return &goCaptchaGenerator{cfg: cfg}
}

func (g *goCaptchaGenerator) init() error {
	g.once.Do(func() {
		font, err := fzshengsksjw.GetFont()
		if err != nil {
			g.initErr = fmt.Errorf("load captcha font: %w", err)
			return
		}
		bgImages, err := imagesv2.GetImages()
		if err != nil {
			g.initErr = fmt.Errorf("load captcha backgrounds: %w", err)
			return
		}
		graphs, err := tiles.GetTiles()
		if err != nil {
			g.initErr = fmt.Errorf("load captcha tiles: %w", err)
			return
		}
		shapeMaps, err := shapes.GetShapes()
		if err != nil {
			g.initErr = fmt.Errorf("load captcha shapes: %w", err)
			return
		}

		verifyLen := g.clickVerifyLen()

		// 文字点选：中文字符，国内用户友好
		clickBuilder := click.NewBuilder(
			click.WithRangeLen(option.RangeVal{Min: verifyLen + 1, Max: verifyLen + 3}),
			click.WithRangeVerifyLen(option.RangeVal{Min: verifyLen, Max: verifyLen}),
		)
		clickBuilder.SetResources(
			click.WithChars(chars.GetChineseChars()),
			click.WithFonts([]*truetype.Font{font}),
			click.WithBackgrounds(bgImages),
		)
		g.clickCapt = clickBuilder.Make()

		// 图形点选：与语言无关，供海外部署使用，不需要字体资源
		shapeBuilder := click.NewBuilder(
			click.WithRangeLen(option.RangeVal{Min: verifyLen + 1, Max: verifyLen + 3}),
			click.WithRangeVerifyLen(option.RangeVal{Min: verifyLen, Max: verifyLen}),
			click.WithIsThumbNonDeformAbility(true),
		)
		shapeBuilder.SetResources(
			click.WithShapes(shapeMaps),
			click.WithBackgrounds(bgImages),
		)
		g.shapeCapt = shapeBuilder.MakeShape()

		tileGraphs := make([]*slide.GraphImage, 0, len(graphs))
		for _, graph := range graphs {
			tileGraphs = append(tileGraphs, &slide.GraphImage{
				OverlayImage: graph.OverlayImage,
				MaskImage:    graph.MaskImage,
				ShadowImage:  graph.ShadowImage,
			})
		}

		// 滑动：拼图块沿固定 Y 轴水平移动
		slideBuilder := slide.NewBuilder()
		slideBuilder.SetResources(
			slide.WithGraphImages(tileGraphs),
			slide.WithBackgrounds(bgImages),
		)
		g.slideCapt = slideBuilder.Make()

		// 拖拽：二维自由拖动，X 与 Y 都要猜中，强度比滑动高约两个数量级
		dragBuilder := slide.NewBuilder(
			slide.WithGenGraphNumber(2),
			slide.WithEnableGraphVerticalRandom(true),
		)
		dragBuilder.SetResources(
			slide.WithGraphImages(tileGraphs),
			slide.WithBackgrounds(bgImages),
		)
		g.dragCapt = dragBuilder.MakeDragDrop()

		rotateBuilder := rotate.NewBuilder(
			rotate.WithRangeAnglePos([]option.RangeVal{{Min: rotateMinAngle, Max: rotateMaxAngle}}),
		)
		rotateBuilder.SetResources(rotate.WithImages(bgImages))
		g.rotateCapt = rotateBuilder.Make()
	})
	return g.initErr
}

func (g *goCaptchaGenerator) clickVerifyLen() int {
	if g.cfg != nil && g.cfg.GoCaptcha.ClickVerifyLen > 0 {
		return g.cfg.GoCaptcha.ClickVerifyLen
	}
	return defaultClickVerifyLen
}

func (g *goCaptchaGenerator) Generate(mode service.GoCaptchaMode) (*service.GoCaptchaChallenge, error) {
	if err := g.init(); err != nil {
		return nil, err
	}
	switch mode {
	case service.GoCaptchaModeShape:
		return generateClickLike(g.shapeCapt)
	case service.GoCaptchaModeSlide:
		return generateSlideLike(g.slideCapt)
	case service.GoCaptchaModeDrag:
		return generateSlideLike(g.dragCapt)
	case service.GoCaptchaModeRotate:
		return g.generateRotate()
	default:
		return generateClickLike(g.clickCapt)
	}
}

// generateClickLike 文字点选与图形点选共用：两者的数据结构与校验方式完全一致。
func generateClickLike(capt click.Captcha) (*service.GoCaptchaChallenge, error) {
	data, err := capt.Generate()
	if err != nil {
		return nil, fmt.Errorf("generate click captcha: %w", err)
	}
	dots := data.GetData()
	if len(dots) == 0 {
		return nil, fmt.Errorf("generate click captcha: empty dots")
	}
	master, err := data.GetMasterImage().ToBase64()
	if err != nil {
		return nil, fmt.Errorf("encode master image: %w", err)
	}
	thumb, err := data.GetThumbImage().ToBase64()
	if err != nil {
		return nil, fmt.Errorf("encode thumb image: %w", err)
	}
	answer, err := json.Marshal(dots)
	if err != nil {
		return nil, fmt.Errorf("marshal click answer: %w", err)
	}
	// 点选模式不下发任何坐标，缩略图本身就是题面
	return &service.GoCaptchaChallenge{MasterImage: master, ThumbImage: thumb, Answer: answer}, nil
}

// generateSlideLike 滑动与拖拽共用：两者都产出 *slide.Block，只是可移动维度不同。
func generateSlideLike(capt slide.Captcha) (*service.GoCaptchaChallenge, error) {
	data, err := capt.Generate()
	if err != nil {
		return nil, fmt.Errorf("generate slide captcha: %w", err)
	}
	block := data.GetData()
	if block == nil {
		return nil, fmt.Errorf("generate slide captcha: empty block")
	}
	master, err := data.GetMasterImage().ToBase64()
	if err != nil {
		return nil, fmt.Errorf("encode master image: %w", err)
	}
	tile, err := data.GetTileImage().ToBase64()
	if err != nil {
		return nil, fmt.Errorf("encode tile image: %w", err)
	}
	answer, err := json.Marshal(block)
	if err != nil {
		return nil, fmt.Errorf("marshal slide answer: %w", err)
	}
	// DX/DY 是拼图块的起始摆放位置，可以下发；X/Y 是缺口位置，是答案，只留在 Answer 里
	return &service.GoCaptchaChallenge{
		MasterImage: master,
		ThumbImage:  tile,
		TileX:       block.DX,
		TileY:       block.DY,
		TileWidth:   block.Width,
		TileHeight:  block.Height,
		Answer:      answer,
	}, nil
}

func (g *goCaptchaGenerator) generateRotate() (*service.GoCaptchaChallenge, error) {
	data, err := g.rotateCapt.Generate()
	if err != nil {
		return nil, fmt.Errorf("generate rotate captcha: %w", err)
	}
	block := data.GetData()
	if block == nil {
		return nil, fmt.Errorf("generate rotate captcha: empty block")
	}
	master, err := data.GetMasterImage().ToBase64()
	if err != nil {
		return nil, fmt.Errorf("encode master image: %w", err)
	}
	thumb, err := data.GetThumbImage().ToBase64()
	if err != nil {
		return nil, fmt.Errorf("encode thumb image: %w", err)
	}
	answer, err := json.Marshal(block)
	if err != nil {
		return nil, fmt.Errorf("marshal rotate answer: %w", err)
	}
	// 前端 Rotate 组件需要缩略图直径；Angle 是答案，只留在 Answer 里
	return &service.GoCaptchaChallenge{
		MasterImage: master,
		ThumbImage:  thumb,
		ThumbSize:   block.Width,
		Answer:      answer,
	}, nil
}

// Validate 校验用户作答。submission 的格式按模式区分：
//   - click / shape : "x1,y1,x2,y2,..."，按点击顺序
//   - slide / drag  : "x,y"
//   - rotate        : "angle"
func (g *goCaptchaGenerator) Validate(
	mode service.GoCaptchaMode,
	answer json.RawMessage,
	submission string,
	padding int,
) bool {
	parts := strings.Split(strings.TrimSpace(submission), ",")

	switch mode {
	case service.GoCaptchaModeSlide, service.GoCaptchaModeDrag:
		if len(parts) != 2 {
			return false
		}
		var block *slide.Block
		if err := json.Unmarshal(answer, &block); err != nil || block == nil {
			return false
		}
		sx, ok1 := parseCoordinate(parts[0])
		sy, ok2 := parseCoordinate(parts[1])
		if !ok1 || !ok2 {
			return false
		}
		return slide.Validate(sx, sy, block.X, block.Y, padding)

	case service.GoCaptchaModeRotate:
		if len(parts) != 1 {
			return false
		}
		var block *rotate.Block
		if err := json.Unmarshal(answer, &block); err != nil || block == nil {
			return false
		}
		angle, ok := parseCoordinate(parts[0])
		if !ok {
			return false
		}
		return rotate.Validate(angle, block.Angle, padding)
	}

	// click / shape 共用同一套点选校验
	var dots map[int]*click.Dot
	if err := json.Unmarshal(answer, &dots); err != nil || len(dots) == 0 {
		return false
	}
	// 长度断言不能省：否则只提交一个点也可能通过
	if len(dots)*2 != len(parts) {
		return false
	}
	// 按索引顺序遍历，点击顺序错误同样判失败
	for i := 0; i < len(dots); i++ {
		dot, ok := dots[i]
		if !ok || dot == nil {
			return false
		}
		sx, ok1 := parseCoordinate(parts[i*2])
		sy, ok2 := parseCoordinate(parts[i*2+1])
		if !ok1 || !ok2 {
			return false
		}
		if !validateGoCaptchaClickPoint(sx, sy, dot, padding) {
			return false
		}
	}
	return true
}

// validateGoCaptchaClickPoint intentionally does not call click.Validate from
// go-captcha v2.0.5: that implementation applies padding only to the right and
// bottom edges. The product contract requires a symmetric tolerance around the
// target rectangle.
func validateGoCaptchaClickPoint(sx, sy int, dot *click.Dot, padding int) bool {
	if dot == nil {
		return false
	}
	if padding < 0 {
		padding = 0
	}
	return sx >= dot.X-padding &&
		sx <= dot.X+dot.Width+padding &&
		sy >= dot.Y-padding &&
		sy <= dot.Y+dot.Height+padding
}

func parseCoordinate(raw string) (int, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	return value, true
}
