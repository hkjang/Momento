package httpapi

import "testing"

func TestRateLimiter(t *testing.T) {
	limiter := newRateLimiter(2)
	if !limiter.Allow("client") || !limiter.Allow("client") {
		t.Fatal("requests inside limit rejected")
	}
	if limiter.Allow("client") {
		t.Fatal("request above limit accepted")
	}
	if !limiter.Allow("other") {
		t.Fatal("buckets were not isolated")
	}
	limiter.SetLimit(1)
	if limiter.Allow("other") {
		t.Fatal("updated limit was not applied")
	}
}
