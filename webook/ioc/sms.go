package ioc

import (
	"os"
	"time"

	"gitee.com/geekbang/basic-go/webook/internal/domain"
	"gitee.com/geekbang/basic-go/webook/internal/repository"
	"gitee.com/geekbang/basic-go/webook/internal/service/sms"

	"gitee.com/geekbang/basic-go/webook/internal/service/sms/failback"
	faultDetect "gitee.com/geekbang/basic-go/webook/internal/service/sms/fault_detect"

	"gitee.com/geekbang/basic-go/webook/internal/service/sms/localsms"
	"gitee.com/geekbang/basic-go/webook/internal/service/sms/ratelimit"

	"gitee.com/geekbang/basic-go/webook/internal/service/sms/tencent"
	"gitee.com/geekbang/basic-go/webook/pkg/limiter"

	rateCounter "gitee.com/geekbang/basic-go/webook/pkg/rate_counter"
	"github.com/redis/go-redis/v9"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	tencentSMS "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
)

func InitSMSService(redisClient redis.Cmdable, smsRequestRepo repository.SmsRequestRepository) sms.Service {
	//return ratelimit.NewRateLimitSMSService(localsms.NewService(), limiter.NewRedisSlidingWindowLimiter())
	service := localsms.NewService()
	// 如果有需要，就可以用这个
	//return initTencentSMSService()

	return initFailBackSMSService(service, redisClient, smsRequestRepo)
}

func initTencentSMSService() sms.Service {
	secretId, ok := os.LookupEnv("SMS_SECRET_ID")
	if !ok {
		panic("找不到腾讯 SMS 的 secret id")
	}
	secretKey, ok := os.LookupEnv("SMS_SECRET_KEY")
	if !ok {
		panic("找不到腾讯 SMS 的 secret key")
	}
	c, err := tencentSMS.NewClient(
		common.NewCredential(secretId, secretKey),
		"ap-nanjing",
		profile.NewClientProfile(),
	)
	if err != nil {
		panic(err)
	}
	return tencent.NewService(c, "1400842696", "妙影科技")
}

func initFailBackSMSService(service sms.Service, redisClient redis.Cmdable, smsRequestRepo repository.SmsRequestRepository) sms.Service {
	const (
		defaultErrorRateThreshold         = 0.9
		defaultErrorRateSlidingWindow     = 5
		defaultErrorRateCountInterval     = 1
		defaultErrorRateMinDataPointLimit = 100

		defaultMaxFailBackRetryConcurrency = 2
		defaultGetRetryRecordsBatchSize    = 10
	)

	defaultErrorRateIntervalUnit := time.Second

	defaultRetryConfig := domain.SmsRequestRetryConfig{
		GetRecordsInterval: time.Duration(10) * time.Minute,
		RetryCountLimit:    3,
		RetryInterval:      time.Duration(10) * time.Minute,
		ProcessTimeLimit:   time.Duration(10) * time.Minute,
		MaxRetryInterval:   time.Duration(30) * time.Minute,
	}

	// 若有需要可改从环境变数读取设定值

	limiter := limiter.NewRedisSlidingWindowLimiter(redisClient, time.Second, 1000)
	baseRateCounter := rateCounter.NewSlidingWindowRateCounter(defaultErrorRateSlidingWindow, defaultErrorRateCountInterval, defaultErrorRateIntervalUnit)
	rateCounter := rateCounter.NewSlidingWindowRateCounterWithMinDataPointLimit(baseRateCounter, defaultErrorRateMinDataPointLimit)
	serviceWithRatelimit := ratelimit.NewRateLimitSMSService(service, limiter)
	serviceWithFaultDetection := faultDetect.NewFaultDetectBySlidingWindowErrorRateSMSService(serviceWithRatelimit, rateCounter, defaultErrorRateThreshold, faultDetect.IsTimeOutError)
	serviceWithFailBack := failback.NewAsyncFailBackSmsService(serviceWithFaultDetection, smsRequestRepo, defaultMaxFailBackRetryConcurrency, defaultRetryConfig, int64(defaultGetRetryRecordsBatchSize))

	return serviceWithFailBack
}
