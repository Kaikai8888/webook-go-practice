package domain

import "time"

type SmsRequest struct {
	Id                      int64
	TplId                   string
	Args                    []string
	Numbers                 []string
	RetryCount              int64
	IsSucceeded             bool
	IsProcessing            bool
	LastRetryTime           int64
	LastStartProcessingTime int64
	Ctime                   int64
	Utime                   int64
}

type SmsRequestRetryConfig struct {
	GetRecordsInterval time.Duration
	RetryCountLimit    int64
	RetryInterval      time.Duration
	ProcessTimeLimit   time.Duration
	MaxRetryInterval   time.Duration // 最多隔多久以后retry, 避免隔很久还送讯息
}

// func (config *SmsRequestRetryConfig) Validate() error {
// 	if config.RetryInterval * config.RetryCountLimit > config.MaxRetryInterval
// if config.ProcessTimeLimit > config.MaxRetryInterval
// if config.ProcessTimeLimit > config.RetryInterval
// }
