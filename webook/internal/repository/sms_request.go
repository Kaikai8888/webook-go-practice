package repository

import (
	"context"
	"encoding/json"

	"gitee.com/geekbang/basic-go/webook/internal/domain"
	"gitee.com/geekbang/basic-go/webook/internal/repository/dao"
)

var (
	ErrSmsRequestNotFound = dao.ErrRecordNotFound
)

type SmsRequestRepository interface {
	Create(ctx context.Context, smsRequest domain.SmsRequest) (int64, error)
	FindRequestToRetry(ctx context.Context, config domain.SmsRequestRetryConfig, limit int) ([]domain.SmsRequest, error)
	MarkAsRetryFailed(ctx context.Context, id int64) error
	MarkAsRetrySucceeded(ctx context.Context, id int64) error
}

var _ SmsRequestRepository = &smsRequestRepository{}

type smsRequestRepository struct {
	smsRequestDao dao.SmsRequestDao
}

func (repo *smsRequestRepository) Create(ctx context.Context, smsRequest domain.SmsRequest) (int64, error) {
	entity, err := repo.toEntity(smsRequest)
	if err != nil {
		return 0, err
	}
	return repo.smsRequestDao.Insert(ctx, entity)
}

func (repo *smsRequestRepository) FindRequestToRetry(ctx context.Context, config domain.SmsRequestRetryConfig, limit int) ([]domain.SmsRequest, error) {
	requests, err := repo.smsRequestDao.FindRetryableRequestAndMarkAsProcessed(ctx, repo.mapRetryConfigToDaoDef(config), limit)
	if err != nil {
		return []domain.SmsRequest{}, err
	}

	results := make([]domain.SmsRequest, 0, len(requests))
	for _, request := range requests {
		result, err := repo.toDomain(request)
		if err != nil {
			return []domain.SmsRequest{}, err
		}
		results = append(results, result)
	}

	return results, nil
}

func (repo *smsRequestRepository) MarkAsRetryFailed(ctx context.Context, id int64) error {
	return repo.smsRequestDao.MarkAsRetryFailed(ctx, id)
}

func (repo *smsRequestRepository) MarkAsRetrySucceeded(ctx context.Context, id int64) error {
	return repo.smsRequestDao.MarkAsRetrySucceeded(ctx, id)
}

func (repo *smsRequestRepository) toEntity(r domain.SmsRequest) (dao.SmsRequest, error) {
	args, err := json.Marshal(r.Args)
	if err != nil {
		return dao.SmsRequest{}, err
	}

	numbers, err := json.Marshal(r.Numbers)
	if err != nil {
		return dao.SmsRequest{}, err
	}

	return dao.SmsRequest{
		Id:                      r.Id,
		TplId:                   r.TplId,
		Args:                    string(args),
		Numbers:                 string(numbers),
		RetryCount:              r.RetryCount,
		IsSucceeded:             r.IsSucceeded,
		IsProcessing:            r.IsProcessing,
		LastRetryTime:           r.LastRetryTime,
		LastStartProcessingTime: r.LastStartProcessingTime,
		Ctime:                   r.Ctime,
		Utime:                   r.Utime,
	}, nil
}

func (repo *smsRequestRepository) toDomain(r dao.SmsRequest) (domain.SmsRequest, error) {
	var args, numbers []string
	if err := json.Unmarshal([]byte(r.Args), &args); err != nil {
		return domain.SmsRequest{}, err
	}

	if err := json.Unmarshal([]byte(r.Numbers), &numbers); err != nil {
		return domain.SmsRequest{}, err
	}

	return domain.SmsRequest{
		Id:                      r.Id,
		TplId:                   r.TplId,
		Args:                    args,
		Numbers:                 numbers,
		RetryCount:              r.RetryCount,
		IsSucceeded:             r.IsSucceeded,
		IsProcessing:            r.IsProcessing,
		LastRetryTime:           r.LastRetryTime,
		LastStartProcessingTime: r.LastStartProcessingTime,
		Ctime:                   r.Ctime,
		Utime:                   r.Utime,
	}, nil
}

func (repo *smsRequestRepository) mapRetryConfigToDaoDef(config domain.SmsRequestRetryConfig) dao.SmsRequestRetryConfig {
	return dao.SmsRequestRetryConfig{
		RetryCountLimit:  config.RetryCountLimit,
		RetryInterval:    config.RetryInterval,
		ProcessTimeLimit: config.ProcessTimeLimit,
		MaxRetryInterval: config.MaxRetryInterval,
	}
}
