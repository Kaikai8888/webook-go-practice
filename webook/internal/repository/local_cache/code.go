package local_cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/allegro/bigcache/v3"
)

const VerifyCountLimit = 3

var Expiration = 10 * time.Minute

var ErrEntryNotFound = bigcache.ErrEntryNotFound
var ErrCodeSendTooMany = errors.New("驗證碼發送太多次")
var ErrCodeVerifyTooMany = errors.New("驗證碼驗證太多次")
var ErrExpirationTimeMissing = errors.New("沒有設過期時間")

type CodeCache interface {
	Set(ctx context.Context, biz, phone, code string) error
	Verify(ctx context.Context, biz, phone, code string) (bool, error)
}

type LocalCache interface {
	Get(key string) ([]byte, error)
	Set(key string, entry []byte) error
	Delete(key string) error
}

type Code struct {
	Code           string    `json:"code"`
	ExpirationTime time.Time `json:"expiration_time"`
	VerifyCount    int       `json:"verify_count"`
}

func (c *Code) IsExpired() bool {
	return c.ExpirationTime.Before(time.Now())
}

func (c *Code) Ttl() time.Duration {
	return time.Until(c.ExpirationTime)
}

type LocalCodeCache struct {
	lock             sync.RWMutex
	cache            LocalCache
	expiration       time.Duration
	verifyCountLimit int
}

func NewCodeCache(cache LocalCache) CodeCache {
	return &LocalCodeCache{
		lock:             sync.RWMutex{},
		cache:            cache,
		expiration:       Expiration,
		verifyCountLimit: VerifyCountLimit,
	}
}

func (c *LocalCodeCache) Set(ctx context.Context, biz, phone, code string) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	key := c.key(biz, phone)
	entry, err := c.cache.Get(key)

	switch err {
	case nil:
		var codeData Code
		if err := json.Unmarshal(entry, &codeData); err != nil {
			return err
		}

		if codeData.ExpirationTime.IsZero() {
			return ErrExpirationTimeMissing
		}
		if !codeData.IsExpired() {
			minSendInternal := math.Max(math.Ceil(c.expiration.Seconds()*0.1), 60)
			durationAfterFirstSend := c.expiration.Seconds() - codeData.Ttl().Seconds()

			if durationAfterFirstSend < minSendInternal {
				return ErrCodeSendTooMany
			}
		}
	case ErrEntryNotFound:
	default:
		return err
	}

	// 1. 没有验证码
	// 2.	验证码过期但尚未删除
	// 3. 有验证码, 但距离上次寄送已超过minSendInternal (至少 1 分钟)
	codeData := Code{
		Code:           code,
		ExpirationTime: time.Now().Add(c.expiration),
	}
	c.setToCache(key, &codeData)
	return nil

}

func (c *LocalCodeCache) Verify(ctx context.Context, biz, phone, code string) (bool, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	key := c.key(biz, phone)
	entry, err := c.cache.Get(key)

	switch err {
	case nil:
	case ErrEntryNotFound:
		return false, nil
	default:
		return false, err
	}

	var codeData Code
	if err := json.Unmarshal(entry, &codeData); err != nil {
		return false, err
	}

	if codeData.IsExpired() {
		return false, ErrEntryNotFound
	}

	if c.ExceedVerifyLimit(&codeData) {
		return false, ErrCodeVerifyTooMany
	}

	codeData.VerifyCount += 1

	if err := c.setToCache(key, &codeData); err != nil {
		return false, err
	}

	verified := codeData.Code == code

	if verified {
		if err := c.cache.Delete(key); err != nil {
			fmt.Printf("Failed to delete verified code, err: %v", err)
		}
	}

	return verified, nil
}

func (c *LocalCodeCache) key(biz, phone string) string {
	return fmt.Sprintf("%s:%s", biz, phone)
}

func (c *LocalCodeCache) ExceedVerifyLimit(code *Code) bool {
	return code.VerifyCount >= c.verifyCountLimit
}

func (c *LocalCodeCache) setToCache(key string, code *Code) error {
	entry, err := json.Marshal(code)
	if err != nil {
		return err
	}

	return c.cache.Set(key, entry)
}
