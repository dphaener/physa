package forge

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRateLimitMessagePrimaryWithReset(t *testing.T) {
	r := &RateLimit{Reset: time.Date(2026, 11, 26, 14, 0, 0, 0, time.UTC)}
	got := r.Error()
	if !strings.Contains(got, "primary rate limit") || !strings.Contains(got, "2026-11-26T14:00:00Z") {
		t.Errorf("message = %q", got)
	}
}

func TestRateLimitMessageSecondaryWithRetryAfter(t *testing.T) {
	r := &RateLimit{Secondary: true, RetryAfter: 60 * time.Second, Detail: "slow down"}
	got := r.Error()
	for _, want := range []string{"secondary rate limit", "1m0s", "slow down"} {
		if !strings.Contains(got, want) {
			t.Errorf("message %q lacks %q", got, want)
		}
	}
}

func TestRateLimitMessageNoHints(t *testing.T) {
	got := (&RateLimit{}).Error()
	if !strings.Contains(got, "rate limit") {
		t.Errorf("message = %q", got)
	}
	if strings.Contains(got, "0001") || strings.Contains(got, "reset") || strings.Contains(got, "retry") {
		t.Errorf("message names a time it does not know: %q", got)
	}
}

func TestRateLimitFoundThroughWrap(t *testing.T) {
	err := fmt.Errorf("page 3: %w", &RateLimit{Secondary: true})
	var r *RateLimit
	if !errors.As(err, &r) || !r.Secondary {
		t.Errorf("errors.As did not find the RateLimit in %v", err)
	}
}
