package esi

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// RateLimited reports that ESI refused a request because a rate limit was
// reached (HTTP 420 "error limited" or 429 "too many requests"). RetryAfter
// is the server-supplied pause (zero when ESI sent no Retry-After header, in
// which case callers fall back to their own backoff). ErrorLimitRemain and
// ErrorLimitReset mirror X-Esi-Error-Limit-* for future use; the preemptive
// error-limit guard is deliberately out of scope (see ADR 0002).
type RateLimited struct {
	RetryAfter       time.Duration
	ErrorLimitRemain int
	ErrorLimitReset  time.Duration
}

func (e *RateLimited) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("ESI rate limited: retry after %s", e.RetryAfter)
	}
	return "ESI rate limited"
}

// HTTPError reports a non-2xx, non-rate-limit ESI response. Callers inspect
// StatusCode to tell permanent failures (404) from retryable ones (5xx).
type HTTPError struct {
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("ESI %s", e.Status)
}

// newRateLimited builds a *RateLimited from a 420/429 response, reading the
// Retry-After and error-limit headers ESI supplies on the error path.
func newRateLimited(resp *http.Response) *RateLimited {
	e := &RateLimited{
		RetryAfter:      headerSeconds(resp.Header, "Retry-After"),
		ErrorLimitReset: headerSeconds(resp.Header, "X-Esi-Error-Limit-Reset"),
	}
	if v := resp.Header.Get("X-Esi-Error-Limit-Remain"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			e.ErrorLimitRemain = n
		}
	}
	return e
}

// headerSeconds parses an integer-seconds header, returning zero when it is
// absent or malformed.
func headerSeconds(h http.Header, key string) time.Duration {
	v := h.Get(key)
	if v == "" {
		return 0
	}
	secs, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return time.Duration(secs) * time.Second
}
