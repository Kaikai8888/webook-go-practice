package dao

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SmsRequestDao interface {
	Insert(context.Context, SmsRequest) (int64, error)
	FindRetryableRequestAndMarkAsProcessed(ctx context.Context, config SmsRequestRetryConfig, limit int) ([]SmsRequest, error)
	MarkAsRetryFailed(ctx context.Context, id int64) error
	MarkAsRetrySucceeded(ctx context.Context, id int64) error
}

type GormSmsRequestDao struct {
	db *gorm.DB
}

func NewSmsRequestDao(db *gorm.DB) SmsRequestDao {
	return &GormSmsRequestDao{
		db: db,
	}
}

func (dao *GormSmsRequestDao) Insert(ctx context.Context, r SmsRequest) (int64, error) {
	now := time.Now().UnixMilli()
	r.Ctime = now
	r.Utime = now
	tx := dao.db.WithContext(ctx).Create(&r)
	// how to get inserted id?

	if err := tx.Error; err != nil {
		return 0, err
	}

	return r.Id, nil
}

func (dao *GormSmsRequestDao) FindRetryableRequestAndMarkAsProcessed(ctx context.Context, config SmsRequestRetryConfig, limit int) ([]SmsRequest, error) {
	requests := []SmsRequest{}
	now := time.Now()

	result := dao.db.WithContext(ctx).Model(&requests).
		Clauses(clause.Returning{}).
		Where("is_successed = false and is_processing = false").
		Where("retry_count < ?", config.RetryCountLimit).
		Where("created_at > ?", now.Add(time.Duration(-1)*config.MaxRetryInterval).UnixMilli()).
		Where("last_retry_time < ?", now.Add(time.Duration(-1)*config.RetryInterval).UnixMilli()).
		First(limit).
		Updates(SmsRequest{IsProcessing: true, LastStartProcessingTime: now.UnixMilli()})
	if result.Error != nil {
		return []SmsRequest{}, result.Error
	}

	return requests, nil

}

func (dao *GormSmsRequestDao) MarkAsRetryFailed(ctx context.Context, id int64) error {
	now := time.Now()

	result := dao.db.WithContext(ctx).Model(&SmsRequest{}).
		Where("id = ?", id).
		First(1).
		Updates(SmsRequest{IsSucceeded: false, IsProcessing: false, LastRetryTime: now.UnixMilli()})
	if result.Error != nil {
		return result.Error
	}

	return nil
}

func (dao *GormSmsRequestDao) MarkAsRetrySucceeded(ctx context.Context, id int64) error {
	now := time.Now()

	result := dao.db.WithContext(ctx).Model(&SmsRequest{}).
		Where("id = ?", id).
		First(1).
		Updates(SmsRequest{IsSucceeded: true, IsProcessing: false, LastRetryTime: now.UnixMilli()})
	if result.Error != nil {
		return result.Error
	}

	return nil
}

type SmsRequest struct {
	Id                      int64  `gorm:"column:id,primaryKey,autoIncrement"`
	TplId                   string `gorm:"column:tpl_id,type=varchar(128)"`
	Args                    string `gorm:"column:arguments,type=text"`
	Numbers                 string `gorm:"column:numbers,type=text"`
	RetryCount              int64  `gorm:"column:retry_count"`
	IsSucceeded             bool   `gorm:"column:is_succeeded"`
	IsProcessing            bool   `gorm:"column:is_processing"`
	LastRetryTime           int64  `gorm:"column:last_retry_time"`
	LastStartProcessingTime int64  `gorm:"column:last_start_processing_time"`
	Ctime                   int64  `gorm:"column:created_at"`
	Utime                   int64  `gorm:"column:updated_at"`
}

type SmsRequestRetryConfig struct {
	RetryCountLimit  int64
	RetryInterval    time.Duration
	ProcessTimeLimit time.Duration
	MaxRetryInterval time.Duration
}
