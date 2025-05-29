package rate_limiter

import (
	"sync/atomic"
	"time"
)

type _bucket struct {
	capacity   int64
	rate       int64
	tokens     int64
	last_refil int64
}

func (b *_bucket) refill() {
	for {
		currentTokenCount := b.tokens
		last_refil := b.last_refil
		now := timestamp()
		deltaTime := now - last_refil
		newTokenCount := min(currentTokenCount+deltaTime*b.rate, b.capacity)
		if !atomic.CompareAndSwapInt64(&b.tokens, currentTokenCount, newTokenCount) {
			continue
		}
		if !atomic.CompareAndSwapInt64(&b.last_refil, last_refil, now) {
			continue
		}
		break
	}
}

func (b *_bucket) useToken() bool {
	b.refill()
	for {
		tokens := b.tokens
		if tokens == 0 {
			return false
		}
		if atomic.CompareAndSwapInt64(&b.tokens, tokens, tokens-1) {
			return true
		}
	}
}

func newBucket(rate int64) *_bucket {
	return &_bucket{
		rate:       rate,
		capacity:   rate * 2,
		tokens:     rate * 2,
		last_refil: timestamp(),
	}
}

func timestamp() int64 {
	return time.Now().Unix()
}
