package rate_limiter

import (
	sl "lorde.tech/tinyurl/skiplist"
)

type RateLimiter struct {
	rate  int64
	ipMap *sl.SkipList[*_bucket]
}

func (rl *RateLimiter) fetchByIp(ip string) (*_bucket, error) {
	found, bucket := rl.ipMap.Search(ip)
	if !found {
		bucket = newBucket(rl.rate)
		if err := rl.ipMap.Insert(ip, bucket); err != nil {
			return nil, err
		}
	}
	return bucket, nil
}

func (rl *RateLimiter) ShouldServe(ip string) bool {
	bucket, err := rl.fetchByIp(ip)
	if err != nil {
		return false
	}
	return bucket.useToken()
}

func NewRateLimiter(rate int64) *RateLimiter {
	return &RateLimiter{rate: rate, ipMap: sl.NewSkiplist[*_bucket]()}
}
