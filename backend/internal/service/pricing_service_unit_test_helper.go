//go:build unit

package service

func NewPricingServiceForTest(data map[string]*ModelPriceEntry) *PricingService {
	return &PricingService{pricingData: data}
}
