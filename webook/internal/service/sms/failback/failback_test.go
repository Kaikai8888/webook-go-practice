package failback

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"gitee.com/geekbang/basic-go/webook/internal/domain"
	"gitee.com/geekbang/basic-go/webook/internal/repository"
	repomocks "gitee.com/geekbang/basic-go/webook/internal/repository/mocks"
	"gitee.com/geekbang/basic-go/webook/internal/service/sms"
	smsmocks "gitee.com/geekbang/basic-go/webook/internal/service/sms/mocks"
	"gitee.com/geekbang/basic-go/webook/internal/service/sms/ratelimit"
)

func TestFaultDetect_SlidingWindow(t *testing.T) {
	const (
		maxConcurrency           = 1
		getRetryRecordsBatchSize = 10
		tplId                    = "mock-tpl-id"
		number                   = "12345678"
	)

	errDb := errors.New("db error")

	config := domain.SmsRequestRetryConfig{
		GetRecordsInterval: time.Duration(1 * time.Second),
		RetryCountLimit:    3,
		RetryInterval:      time.Duration(1 * time.Second),
		ProcessTimeLimit:   time.Duration(1 * time.Second),
		MaxRetryInterval:   time.Duration(3 * time.Second),
	}

	testCases := []struct {
		name       string
		mocks      func(ctrl *gomock.Controller) (sms.Service, repository.SmsRequestRepository)
		wait       time.Duration
		wantResult error
	}{
		{
			name: "successfully send sms",
			mocks: func(ctrl *gomock.Controller) (sms.Service, repository.SmsRequestRepository) {
				smsSvc := smsmocks.NewMockService(ctrl)
				smsSvc.EXPECT().Send(gomock.Any(), tplId, gomock.Any(), number).
					Return(nil)

				repo := repomocks.NewMockSmsRequestRepository(ctrl)

				return smsSvc, repo
			},
			wantResult: nil,
		}, {
			name: "ratelimit.ErrLimited occurs, failed to save request to db, return error without retry",
			mocks: func(ctrl *gomock.Controller) (sms.Service, repository.SmsRequestRepository) {
				smsSvc := smsmocks.NewMockService(ctrl)
				smsSvc.EXPECT().Send(gomock.Any(), tplId, gomock.Any(), number).
					Return(ratelimit.ErrLimited).Times(1)

				repo := repomocks.NewMockSmsRequestRepository(ctrl)
				repo.EXPECT().Create(gomock.Any(), domain.SmsRequest{
					TplId:   tplId,
					Numbers: []string{number},
				}).Return(int64(0), errDb).Times(1)

				return smsSvc, repo
			},
			wantResult: errDb,
			wait:       config.GetRecordsInterval * 2,
		}, {
			name: "ratelimit.ErrLimited occurs, retry once and success",
			mocks: func(ctrl *gomock.Controller) (sms.Service, repository.SmsRequestRepository) {
				smsSvc := smsmocks.NewMockService(ctrl)
				firstCall := smsSvc.EXPECT().Send(gomock.Any(), tplId, gomock.Any(), number).
					Return(ratelimit.ErrLimited)
				smsSvc.EXPECT().Send(gomock.Any(), tplId, gomock.Any(), number).
					Return(nil).After(firstCall)

				repo := repomocks.NewMockSmsRequestRepository(ctrl)
				repo.EXPECT().Create(gomock.Any(), domain.SmsRequest{
					TplId:   tplId,
					Numbers: []string{number},
				}).Return(int64(1), nil).Times(1)

				firstFind := repo.EXPECT().FindRequestToRetry(gomock.Any(), config, getRetryRecordsBatchSize).Return([]domain.SmsRequest{{
					Id:      int64(1),
					TplId:   tplId,
					Numbers: []string{number},
				}}, nil)
				repo.EXPECT().FindRequestToRetry(gomock.Any(), config, getRetryRecordsBatchSize).Return([]domain.SmsRequest{}, repository.ErrSmsRequestNotFound).After(firstFind).MinTimes(1)

				repo.EXPECT().MarkAsRetrySucceeded(gomock.Any(), int64(1)).Return(nil).Times(1)
				return smsSvc, repo
			},
			wantResult: ErrAutoRetryLatter,
			wait:       config.GetRecordsInterval * 3,
		}, {
			name: "ratelimit.ErrLimited occurs, retry twice and success",
			mocks: func(ctrl *gomock.Controller) (sms.Service, repository.SmsRequestRepository) {
				smsSvc := smsmocks.NewMockService(ctrl)
				firstCall := smsSvc.EXPECT().Send(gomock.Any(), tplId, gomock.Any(), number).
					Return(ratelimit.ErrLimited).Times(2)
				smsSvc.EXPECT().Send(gomock.Any(), tplId, gomock.Any(), number).
					Return(nil).After(firstCall)

				repo := repomocks.NewMockSmsRequestRepository(ctrl)
				repo.EXPECT().Create(gomock.Any(), domain.SmsRequest{
					TplId:   tplId,
					Numbers: []string{number},
				}).Return(int64(1), nil).Times(1)

				firstFind := repo.EXPECT().FindRequestToRetry(gomock.Any(), config, getRetryRecordsBatchSize).Return([]domain.SmsRequest{{
					Id:      int64(1),
					TplId:   tplId,
					Numbers: []string{number},
				}}, nil)
				secondFind := repo.EXPECT().FindRequestToRetry(gomock.Any(), config, getRetryRecordsBatchSize).Return([]domain.SmsRequest{{
					Id:         int64(1),
					TplId:      tplId,
					Numbers:    []string{number},
					RetryCount: 1,
				}}, nil).After(firstFind)
				repo.EXPECT().FindRequestToRetry(gomock.Any(), config, getRetryRecordsBatchSize).Return([]domain.SmsRequest{}, repository.ErrSmsRequestNotFound).After(secondFind).MinTimes(1)

				repo.EXPECT().MarkAsRetrySucceeded(gomock.Any(), int64(1)).Return(nil).Times(1)
				repo.EXPECT().MarkAsRetryFailed(gomock.Any(), int64(1)).Return(nil).Times(1)

				return smsSvc, repo
			},
			wantResult: ErrAutoRetryLatter,
			wait:       config.GetRecordsInterval * 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			smsSvc, repo := tc.mocks(ctrl)

			svc := NewAsyncFailBackSmsService(smsSvc, repo, maxConcurrency, config, getRetryRecordsBatchSize)
			err := svc.Send(context.Background(), tplId, nil, number)
			if err != tc.wantResult {
				t.Errorf("want %v, but got %v", tc.wantResult, err)
			}

			if tc.wait > 0 {
				time.Sleep(tc.wait)
			}
		})
	}
}
