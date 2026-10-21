package fault_detect

import (
	"context"
	"errors"
	"math/rand"

	"gitee.com/geekbang/basic-go/webook/internal/service/sms"
	rateCounter "gitee.com/geekbang/basic-go/webook/pkg/rate_counter"
)

var (
	ErrThirdPartyProviderCrash = errors.New("third party provider crash down")
)

type FaultDetectBySlidingWindowErrorRateSMSService struct {
	smsSvc        sms.Service
	rateCounter   rateCounter.RateCounter
	threshold     float64
	errorDetector func(error) bool
}

func NewFaultDetectBySlidingWindowErrorRateSMSService(smsSvc sms.Service, rateCounter rateCounter.RateCounter, threshold float64, errDetector func(error) bool) sms.Service {
	return &FaultDetectBySlidingWindowErrorRateSMSService{
		smsSvc:        smsSvc,
		rateCounter:   rateCounter,
		threshold:     threshold,
		errorDetector: errDetector,
	}
}

func (s *FaultDetectBySlidingWindowErrorRateSMSService) Send(ctx context.Context, tplId string, args []string, numbers ...string) error {
	// 寄送前先检查错误率, 若已达threshold, 则随机抽样, 10%的流量仍继续尝试发简讯, 90%流量直接视为第三方已崩溃
	if rate := s.rateCounter.GetRate(); rate > s.threshold && rand.Float64() > 0.1 {
		return ErrThirdPartyProviderCrash
	}

	if err := s.smsSvc.Send(ctx, tplId, args, numbers...); err != nil {
		if s.errorDetector(err) {
			if rate, err := s.rateCounter.Add(true); err != nil {
				return err
			} else if rate > s.threshold {
				return ErrThirdPartyProviderCrash
			}
		}
		return err
	}
	return nil
}

func IsTimeOutError(err error) bool {
	switch err {
	case context.Canceled, context.DeadlineExceeded:
		// 前者是被取消，后者是超时
		return true
	}
	return false
}
