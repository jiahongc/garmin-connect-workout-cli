// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"garmin-connect-workout-cli/internal/garminsession"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

var garminBrowserMu sync.Mutex

type browserPostResponse struct {
	BaseURL string `json:"base_url"`
	Status  int    `json:"status"`
	Body    string `json:"body"`
}

func garminBrowserGetJSON(ctx context.Context, path string) ([]byte, int, error) {
	return garminBrowserRequestJSON(ctx, "GET", path, nil)
}

func garminBrowserPostJSON(ctx context.Context, path string, body any) ([]byte, int, error) {
	return garminBrowserRequestJSON(ctx, "POST", path, body)
}

func garminBrowserPutJSON(ctx context.Context, path string, body any) ([]byte, int, error) {
	return garminBrowserRequestJSON(ctx, "PUT", path, body)
}

func garminBrowserDelete(ctx context.Context, path string) ([]byte, int, error) {
	return garminBrowserRequestJSON(ctx, "DELETE", path, nil)
}

func garminBrowserRequestJSON(ctx context.Context, method string, path string, body any) ([]byte, int, error) {
	return garminBrowserRequestJSONWithHeadless(ctx, method, path, body, garminBrowserDefaultHeadless())
}

func garminBrowserRequestJSONWithHeadless(ctx context.Context, method string, path string, body any, headless bool) ([]byte, int, error) {
	garminBrowserMu.Lock()
	defer garminBrowserMu.Unlock()

	profileDir, err := garminsession.BrowserProfileDir()
	if err != nil {
		return nil, 0, err
	}
	if _, err := os.Stat(filepath.Join(profileDir, "Default", "Cookies")); err != nil {
		return nil, 0, fmt.Errorf("Garmin browser profile is not ready; run auth login-browser first")
	}
	pathLiteral, err := json.Marshal(path)
	if err != nil {
		return nil, 0, err
	}
	methodLiteral, err := json.Marshal(method)
	if err != nil {
		return nil, 0, err
	}
	bodyLiteral, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", headless),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.UserDataDir(profileDir),
		chromedp.WindowSize(1280, 900),
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()
	browserCtx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	browserCtx, timeoutCancel := context.WithTimeout(browserCtx, 90*time.Second)
	defer timeoutCancel()

	var parsed browserPostResponse
	script := fmt.Sprintf(`(async () => {
		const method = %s;
		const path = %s;
		const body = %s;
		const csrf = document.querySelector("meta[name='csrf-token']")?.getAttribute("content") || "";
		let token = null;
		try {
			token = JSON.parse(localStorage.getItem("token") || "null");
		} catch (_) {
			token = null;
		}
		const bases = [
			"/gc-api",
			"https://connectapi.garmin.com",
			"https://connect.garmin.com/proxy",
			"https://connect.garmin.com/modern/proxy"
		];
		let last = {base_url: "", status: 0, body: ""};
		for (const base of bases) {
			const options = {
				method,
				credentials: "include",
				headers: {
					"Accept": "application/json",
					"Content-Type": "application/json",
					...(token?.access_token ? {"Authorization": "Bearer " + token.access_token} : {}),
					"Connect-Csrf-Token": csrf,
					"NK": "NT",
					"Referer": "https://connect.garmin.com/app/workouts"
				}
			};
			if (body !== null) {
				options.body = JSON.stringify(body);
			}
			const response = await fetch(base + path, {
				...options
			});
			const responseBody = await response.text();
			last = {base_url: base, status: response.status, body: responseBody};
			if (!responseBody.trimStart().toLowerCase().startsWith("<!doctype html") &&
				!responseBody.trimStart().toLowerCase().startsWith("<html")) {
				return last;
			}
		}
		return last;
	})()`, string(methodLiteral), string(pathLiteral), string(bodyLiteral))
	err = chromedp.Run(browserCtx,
		chromedp.Navigate("https://connect.garmin.com/app/workouts"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			value, exception, err := runtime.Evaluate(script).
				WithAwaitPromise(true).
				WithReturnByValue(true).
				Do(ctx)
			if err != nil {
				return err
			}
			if exception != nil {
				return exception
			}
			return json.Unmarshal(value.Value, &parsed)
		}),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("posting through Garmin browser session: %w", err)
	}
	if parsed.Status < 200 || parsed.Status >= 300 {
		return []byte(parsed.Body), parsed.Status, garminBrowserHTTPError(method, path, parsed)
	}
	if bodyLooksLikeHTML(parsed.Body) {
		return []byte(parsed.Body), parsed.Status, authErr(fmt.Errorf("Garmin browser %s %s returned Garmin app HTML, not API JSON; run auth login-browser again", method, path))
	}
	return []byte(parsed.Body), parsed.Status, nil
}

func garminBrowserHTTPError(method string, path string, parsed browserPostResponse) error {
	bodyText := strings.TrimSpace(parsed.Body)
	if bodyText == "" {
		bodyText = "<empty body>"
	}
	if parsed.Status == 401 || parsed.Status == 403 {
		return authErr(fmt.Errorf("Garmin browser session is not authenticated; run auth login-browser again"))
	}
	err := fmt.Errorf("Garmin browser %s %s via %s returned HTTP %d: %s", method, path, parsed.BaseURL, parsed.Status, bodyText)
	if parsed.Status == 429 {
		return rateLimitErr(fmt.Errorf("%w\nhint: Garmin or Cloudflare is rate limiting workout writes. %s", err, garminRateLimitRetryHint(parsed.Body)))
	}
	return apiErr(err)
}

func garminRateLimitRetryHint(body string) string {
	var parsed struct {
		RetryAfter int `json:"retry_after"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err == nil && parsed.RetryAfter > 0 {
		return fmt.Sprintf("Wait at least %d seconds from retry_after before trying one saved draft again.", parsed.RetryAfter)
	}
	return "Wait for the cooldown to clear before trying one saved draft again."
}

func garminBrowserDefaultHeadless() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("GARMIN_CONNECT_BROWSER_HEADLESS")))
	return value != "0" && value != "false" && value != "no"
}

func bodyLooksLikeHTML(body string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(body))
	return strings.HasPrefix(trimmed, "<!doctype html") || strings.HasPrefix(trimmed, "<html")
}
