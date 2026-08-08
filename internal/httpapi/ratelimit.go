package httpapi

import (
	"sync"
	"time"
)

type rateBucket struct {
	minute int64
	count  int
}
type rateLimiter struct {
	mu      sync.Mutex
	limit   int
	buckets map[string]rateBucket
}

func newRateLimiter(limit int) *rateLimiter {
	return &rateLimiter{limit: limit, buckets: map[string]rateBucket{}}
}
func (l *rateLimiter) SetLimit(limit int) {
	if limit < 1 {
		limit = 1
	}
	l.mu.Lock()
	l.limit = limit
	l.mu.Unlock()
}
func (l *rateLimiter) Allow(key string) bool {
	minute := time.Now().Unix() / 60
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b.minute != minute {
		b = rateBucket{minute: minute}
	}
	b.count++
	l.buckets[key] = b
	if len(l.buckets) > 100000 {
		for k, v := range l.buckets {
			if v.minute < minute-1 {
				delete(l.buckets, k)
			}
		}
	}
	return b.count <= l.limit
}
