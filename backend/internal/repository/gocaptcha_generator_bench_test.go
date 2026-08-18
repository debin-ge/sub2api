package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// BenchmarkGoCaptchaGenerate 挑战生成是 CPU 密集的图像合成，也是新引入的 DoS 面。
// 这个基准用于守住 NFR-1（P99 <= 150ms），并在调整素材或尺寸后快速回归。
func BenchmarkGoCaptchaGenerate(b *testing.B) {
	modes := []service.GoCaptchaMode{
		service.GoCaptchaModeClick,
		service.GoCaptchaModeShape,
		service.GoCaptchaModeSlide,
		service.GoCaptchaModeDrag,
		service.GoCaptchaModeRotate,
	}

	for _, mode := range modes {
		b.Run(string(mode), func(b *testing.B) {
			generator := newGoCaptchaGeneratorForTest()
			// 预热，把懒初始化的素材加载排除在计时之外
			if _, err := generator.Generate(mode); err != nil {
				b.Fatalf("warm up %s: %v", mode, err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := generator.Generate(mode); err != nil {
					b.Fatalf("generate %s: %v", mode, err)
				}
			}
		})
	}
}
