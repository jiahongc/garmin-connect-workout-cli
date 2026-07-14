// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"strings"
	"testing"

	"github.com/chromedp/cdproto/network"
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

func TestCaptureGarminSessionHeaders(t *testing.T) {
	capture := &webSessionCapture{}
	captureGarminSessionHeaders(capture, network.Headers{
		"Authorization": "Bearer token",
		"Cookie":        "SESSIONID=cookie",
		"User-Agent":    "test-agent",
	})
	if capture.authorization != "Bearer token" || capture.cookie != "SESSIONID=cookie" || capture.userAgent != "test-agent" {
		t.Fatalf("captured headers = %#v", capture)
	}
}

func TestGarminLoginProbeOnlyRunsFromConnectApp(t *testing.T) {
	for _, tt := range []struct {
		location string
		want     bool
	}{
		{"", false},
		{"https://sso.garmin.com/portal/sso/en-US/sign-in", false},
		{"https://connect.garmin.com/app/workouts", true},
	} {
		if got := shouldProbeGarminLogin(tt.location); got != tt.want {
			t.Fatalf("shouldProbeGarminLogin(%q) = %v, want %v", tt.location, got, tt.want)
		}
	}
}

func TestShouldStopGarminBrowserFallback(t *testing.T) {
	tests := []struct {
		name string
		resp browserPostResponse
		want bool
	}{
		{
			name: "successful JSON response",
			resp: browserPostResponse{Status: 200, Body: `[]`},
			want: true,
		},
		{
			name: "Garmin 427 tries the next base",
			resp: browserPostResponse{Status: 427, Body: `{"error":{"status-code":"427"}}`},
			want: false,
		},
		{
			name: "rate limit stops fallback fanout",
			resp: browserPostResponse{Status: 429, Body: `{"retry_after":30}`},
			want: true,
		},
		{
			name: "HTML response tries the next base",
			resp: browserPostResponse{Status: 200, Body: `<!doctype html><html></html>`},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldStopGarminBrowserFallback(tt.resp); got != tt.want {
				t.Fatalf("shouldStopGarminBrowserFallback(%+v) = %v, want %v", tt.resp, got, tt.want)
			}
		})
	}
}
