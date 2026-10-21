package fault_detect

import (
	"context"
	"testing"

	"gitee.com/geekbang/basic-go/webook/internal/service/sms"
	smsmocks "gitee.com/geekbang/basic-go/webook/internal/service/sms/mocks"
	rateCounter "gitee.com/geekbang/basic-go/webook/pkg/rate_counter"
	rateCounterMocks "gitee.com/geekbang/basic-go/webook/pkg/rate_counter/mocks"
	"go.uber.org/mock/gomock"
)

func TestFaultDetect_SlidingWindow(t *testing.T) {
	const threshold = 0.5
	testCases := []struct {
		name       string
		mocks      func(ctrl *gomock.Controller) (sms.Service, rateCounter.RateCounter)
		wantResult error
	}{
		{
			name: "successfully send sms",
			mocks: func(ctrl *gomock.Controller) (sms.Service, rateCounter.RateCounter) {
				smsSvc := smsmocks.NewMockService(ctrl)
				smsSvc.EXPECT().Send(gomock.Any(),
					gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil)

				rateCounterSvc := rateCounterMocks.NewMockRateCounter(ctrl)
				rateCounterSvc.EXPECT().GetRate().Return(threshold)
				return smsSvc, rateCounterSvc
			},
			wantResult: nil,
		}, {
			name: "context.Canceled error, below threshold",
			mocks: func(ctrl *gomock.Controller) (sms.Service, rateCounter.RateCounter) {
				smsSvc := smsmocks.NewMockService(ctrl)
				smsSvc.EXPECT().Send(gomock.Any(),
					gomock.Any(), gomock.Any(), gomock.Any()).
					Return(context.Canceled)

				rateCounterSvc := rateCounterMocks.NewMockRateCounter(ctrl)
				rateCounterSvc.EXPECT().GetRate().Return(threshold)
				rateCounterSvc.EXPECT().Add(true).Return(threshold-0.1, nil)
				return smsSvc, rateCounterSvc
			},
			wantResult: context.Canceled,
		}, {
			name: "context.Canceled error, above threshold",
			mocks: func(ctrl *gomock.Controller) (sms.Service, rateCounter.RateCounter) {
				smsSvc := smsmocks.NewMockService(ctrl)
				smsSvc.EXPECT().Send(gomock.Any(),
					gomock.Any(), gomock.Any(), gomock.Any()).
					Return(context.Canceled)

				rateCounterSvc := rateCounterMocks.NewMockRateCounter(ctrl)
				rateCounterSvc.EXPECT().GetRate().Return(threshold)
				rateCounterSvc.EXPECT().Add(true).Return(threshold+0.1, nil)
				return smsSvc, rateCounterSvc
			},
			wantResult: ErrThirdPartyProviderCrash,
		}, {
			name: "context.DeadlineExceeded error, below threshold",
			mocks: func(ctrl *gomock.Controller) (sms.Service, rateCounter.RateCounter) {
				smsSvc := smsmocks.NewMockService(ctrl)
				smsSvc.EXPECT().Send(gomock.Any(),
					gomock.Any(), gomock.Any(), gomock.Any()).
					Return(context.DeadlineExceeded)

				rateCounterSvc := rateCounterMocks.NewMockRateCounter(ctrl)
				rateCounterSvc.EXPECT().GetRate().Return(threshold)
				rateCounterSvc.EXPECT().Add(true).Return(threshold-0.1, nil)
				return smsSvc, rateCounterSvc
			},
			wantResult: context.DeadlineExceeded,
		}, {
			name: "context.DeadlineExceeded error, above threshold",
			mocks: func(ctrl *gomock.Controller) (sms.Service, rateCounter.RateCounter) {
				smsSvc := smsmocks.NewMockService(ctrl)
				smsSvc.EXPECT().Send(gomock.Any(),
					gomock.Any(), gomock.Any(), gomock.Any()).
					Return(context.DeadlineExceeded)

				rateCounterSvc := rateCounterMocks.NewMockRateCounter(ctrl)
				rateCounterSvc.EXPECT().GetRate().Return(threshold)
				rateCounterSvc.EXPECT().Add(true).Return(threshold+0.1, nil)
				return smsSvc, rateCounterSvc
			},
			wantResult: ErrThirdPartyProviderCrash,
		}, {
			name: "current rate already exists threshold",
			mocks: func(ctrl *gomock.Controller) (sms.Service, rateCounter.RateCounter) {
				smsSvc := smsmocks.NewMockService(ctrl)

				rateCounterSvc := rateCounterMocks.NewMockRateCounter(ctrl)
				rateCounterSvc.EXPECT().GetRate().Return(threshold + 0.1)
				return smsSvc, rateCounterSvc
			},
			wantResult: ErrThirdPartyProviderCrash,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			smsSvc, rateCounterSvc := tc.mocks(ctrl)
			svc := NewFaultDetectBySlidingWindowErrorRateSMSService(smsSvc, rateCounterSvc, threshold, IsTimeOutError)
			err := svc.Send(context.Background(), "mock-tpl-id", nil, "12345678")
			if err != tc.wantResult {
				t.Errorf("want %v, but got %v", tc.wantResult, err)
			}
		})
	}
}
