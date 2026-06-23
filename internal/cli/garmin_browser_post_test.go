// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"strings"
	"testing"
)

func TestGarminBrowserHTTPErrorRateLimit(t *testing.T) {
	err := garminBrowserHTTPError("POST", "/workout-service/workout", browserPostResponse{
		BaseURL: "/gc-api",
		Status:  429,
		Body:    `{"error_name":"rate_limited","retry_after":30}`,
	})
	if got := ExitCode(err); got != 7 {
		t.Fatalf("ExitCode(rate limit) = %d, want 7", got)
	}
	msg := err.Error()
	for _, want := range []string{"HTTP 429", "rate limiting", "retry_after", "30 seconds"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("rate-limit error = %q, want substring %q", msg, want)
		}
	}
}
