package failback

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"gitee.com/geekbang/basic-go/webook/internal/domain"
	"gitee.com/geekbang/basic-go/webook/internal/repository"
	"gitee.com/geekbang/basic-go/webook/internal/service/sms"
	faultDetect "gitee.com/geekbang/basic-go/webook/internal/service/sms/fault_detect"
	"gitee.com/geekbang/basic-go/webook/internal/service/sms/ratelimit"
)

var (
	ErrAutoRetryLatter = errors.New("errors occur, will retry latter")
)

type AsyncFailBackSmsService struct {
	smsSvc         sms.Service
	smsRequestRepo repository.SmsRequestRepository

	// retry config
	maxConcurrency           int64
	activatedGoroutineCount  int64
	retryConfig              domain.SmsRequestRetryConfig
	getRetryRecordsBatchSize int64
}

func NewAsyncFailBackSmsService(smsSvc sms.Service, smsRequestRepo repository.SmsRequestRepository, maxConcurrency int64, config domain.SmsRequestRetryConfig, getRetryRecordsBatchSize int64) sms.Service {
	return &AsyncFailBackSmsService{
		smsSvc:                   smsSvc,
		smsRequestRepo:           smsRequestRepo,
		maxConcurrency:           maxConcurrency,
		retryConfig:              config,
		getRetryRecordsBatchSize: getRetryRecordsBatchSize,
	}
}

func (s *AsyncFailBackSmsService) Send(ctx context.Context, tplId string, args []string, numbers ...string) error {
	err := s.smsSvc.Send(ctx, tplId, args, numbers...)

	switch err {
	case ratelimit.ErrLimited, faultDetect.ErrThirdPartyProviderCrash:
		// save record to db
		request := domain.SmsRequest{
			TplId:   tplId,
			Args:    args,
			Numbers: numbers,
		}
		// 存不进db就无法retry, 直接回传错误, 不能视为之后会重试
		if _, err := s.smsRequestRepo.Create(ctx, request); err != nil {
			return err
		}

		// start retry background job if not reaching limit
		count := atomic.LoadInt64(&s.activatedGoroutineCount)
		if count < s.maxConcurrency {
			if atomic.CompareAndSwapInt64(&s.activatedGoroutineCount, count, count+1) {
				// 因这传入的是api request的gin context, 而不是main process的context, 因此只能另外间听当前goroutine有无收到shutdown的signal
				currentProcessCtx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

				go func(ctx context.Context) {
					for {
						select {
						case <-ctx.Done():
							return
						default:
							if err := s.retrySend(ctx); err == repository.ErrSmsRequestNotFound {
								/* 没有需要retry的sms request就可结束cronjob
								 * 如果一直无法更新activatedGoroutineCount, 就继续background job, 至少确保有background job可以处理retry
								 */
								for i := 0; i < 3; i++ {
									count := atomic.LoadInt64(&s.activatedGoroutineCount)
									if atomic.CompareAndSwapInt64(&s.activatedGoroutineCount, count, count-1) {
										return
									}
									time.Sleep(1 * time.Second)
								}
							}

						}
					}
				}(currentProcessCtx) // 只能监听到当前goroutine收到的signal, 只有在service是在main goroutine上执行时, 才能做到main process收到signal后到实际shutdown前不会再retry, 避免retry了但还未更新db资料, 就被shutdown, 而造成后续重复retry, 发送讯息
			}
		}
		return ErrAutoRetryLatter
	}

	return err
}

func (s *AsyncFailBackSmsService) retrySend(ctx context.Context) error {
	time.Sleep(s.retryConfig.GetRecordsInterval)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			requests, err := s.smsRequestRepo.FindRequestToRetry(ctx, s.retryConfig, int(s.getRetryRecordsBatchSize))
			if err == repository.ErrSmsRequestNotFound {
				fmt.Printf("no request records found from db to retry")
				return err
			}
			if err != nil {
				fmt.Printf("failed to get request records from db to retry")
				return err
			}

			if len(requests) == 0 {
				return nil
			}

			for _, request := range requests {
				s.retrySendOneRequest(ctx, request)
			}
		}
	}
}

func (s *AsyncFailBackSmsService) retrySendOneRequest(ctx context.Context, request domain.SmsRequest) error {
	fmt.Printf("start processing request, id: %d", request.Id)
	if err := s.sendWithRequestRecord(ctx, request); err != nil {
		fmt.Printf("failed to resend request as processing, skip to next one. id: %d, error: %v", request.Id, err)
		if err := s.smsRequestRepo.MarkAsRetryFailed(ctx, request.Id); err != nil {
			fmt.Printf("failed to mark failed retry, skip to next one. id: %d, error: %v", request.Id, err)
			return err
		}
		return err
	}

	if err := s.smsRequestRepo.MarkAsRetrySucceeded(ctx, request.Id); err != nil {
		fmt.Printf("failed to mark successful retry, skip to next one, id: %d, error: %v", request.Id, err)
		return err
	}

	return nil
}

func (s *AsyncFailBackSmsService) sendWithRequestRecord(ctx context.Context, request domain.SmsRequest) error {
	err := s.smsSvc.Send(ctx, request.TplId, request.Args, request.Numbers...)

	switch err {
	case ratelimit.ErrLimited, faultDetect.ErrThirdPartyProviderCrash:

		return ErrAutoRetryLatter
	}
	return err
}
