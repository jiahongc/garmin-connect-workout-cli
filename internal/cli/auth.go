// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"garmin-connect-workout-cli/internal/cliutil"
	"garmin-connect-workout-cli/internal/garminsession"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
	"github.com/spf13/cobra"
)

func newAuthCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication for Garmin Connect Workouts",
		RunE:  parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newAuthSetupCmd(flags))
	cmd.AddCommand(newAuthLoginBrowserCmd(flags))
	cmd.AddCommand(newAuthStatusCmd(flags))
	cmd.AddCommand(newAuthLogoutCmd(flags))

	return cmd
}

func newAuthLoginBrowserCmd(flags *rootFlags) *cobra.Command {
	var timeout time.Duration
	var profileDir string
	cmd := &cobra.Command{
		Use:   "login-browser",
		Short: "Login through Garmin Connect in a browser",
		Long:  "Checks the saved Garmin login headlessly. Opens Chrome only if sign-in or MFA is needed, then verifies that a fresh headless session can access workouts before reporting success.",
		Example: strings.Join([]string{
			"  garmin-connect-workout-cli auth login-browser",
			"  garmin-connect-workout-cli auth login-browser --timeout 5m",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if timeout <= 0 {
				return usageErr(fmt.Errorf("--timeout must be positive"))
			}
			if profileDir == "" {
				dir, err := garminsession.BrowserProfileDir()
				if err != nil {
					return configErr(err)
				}
				profileDir = dir
			}
			if !filepath.IsAbs(profileDir) {
				return usageErr(fmt.Errorf("--profile-dir must be an absolute path"))
			}
			if err := os.MkdirAll(profileDir, 0o700); err != nil {
				return configErr(fmt.Errorf("creating browser profile dir: %w", err))
			}
			if err := os.Chmod(profileDir, 0o700); err != nil {
				return configErr(fmt.Errorf("securing browser profile dir: %w", err))
			}

			saved, _, _, err := garminsession.Load()
			if err != nil {
				return configErr(err)
			}
			var restore garminsession.Session
			if saved != nil {
				restore = *saved
			}
			interactive := !flags.noInput && !flags.agent && !flags.asJSON
			session, err := ensureGarminBrowserSession(cmd.ErrOrStderr(), interactive, func(headless bool) (garminsession.Session, error) {
				if headless {
					return verifySavedGarminBrowserProfile(cmd.Context(), profileDir, restore, timeout)
				}
				fresh, err := verifyGarminBrowserProfile(cmd.Context(), profileDir, timeout)
				if err == nil {
					restore = fresh
				}
				return fresh, err
			})
			if err != nil {
				return err
			}
			sessionPath, err := garminsession.Save(session)
			if err != nil {
				return authErr(fmt.Errorf("capturing Garmin browser session: %w", err))
			}
			out := map[string]any{
				"authenticated":   true,
				"browser_profile": profileDir,
				"web_session":     sessionPath,
				"verified_at":     time.Now().UTC(),
			}
			if flags.asJSON || flags.agent {
				return printJSONFiltered(cmd.OutOrStdout(), out, flags)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Garmin is ready. Saved login verified in a fresh headless browser.")
			fmt.Fprintln(cmd.OutOrStdout(), "Workout commands will reuse this login without opening Chrome.")
			fmt.Fprintln(cmd.OutOrStdout(), "Next: garmin-connect-workout-cli preferences setup (optional recovery defaults).")
			fmt.Fprintf(cmd.OutOrStdout(), "Browser profile: %s\n", profileDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Web session: %s\n", sessionPath)
			return nil
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Maximum time to wait for browser login")
	cmd.Flags().StringVar(&profileDir, "profile-dir", "", "Browser profile directory for Garmin login cookies")
	return cmd
}

func ensureGarminBrowserSession(w io.Writer, interactive bool, verify func(headless bool) (garminsession.Session, error)) (garminsession.Session, error) {
	fmt.Fprintln(w, "Checking your saved Garmin login in the background...")
	session, err := verify(true)
	if err == nil || ExitCode(err) != 4 {
		return session, err
	}
	if !interactive {
		return garminsession.Session{}, authErr(fmt.Errorf("Garmin sign-in is required. Run garmin-connect-workout-cli auth login-browser without --agent, --json or --no-input to sign in and complete MFA in Chrome"))
	}
	fmt.Fprintln(w, "Opening Chrome for Garmin sign-in. Enter your password and MFA only in that window.")
	fmt.Fprintln(w, "The window closes automatically after verification; then we check that your saved login works in the background.")
	if _, err := verify(false); err != nil {
		return garminsession.Session{}, err
	}
	fmt.Fprintln(w, "Checking that your login survives closing Chrome...")
	session, err = verify(true)
	if err != nil {
		return garminsession.Session{}, fmt.Errorf("Garmin login could not be reused after Chrome closed; setup is not complete: %w", err)
	}
	return session, nil
}

func verifySavedGarminBrowserProfile(ctx context.Context, profileDir string, saved garminsession.Session, timeout time.Duration) (garminsession.Session, error) {
	if _, err := os.Stat(filepath.Join(profileDir, "Default", "Cookies")); os.IsNotExist(err) {
		return garminsession.Session{}, authErr(fmt.Errorf("no saved Garmin browser login"))
	} else if err != nil {
		return garminsession.Session{}, err
	}
	var session garminsession.Session
	err := runGarminBrowserWithSession(ctx, profileDir, saved, true, timeout, func(browserCtx context.Context) error {
		capture := &webSessionCapture{}
		startGarminSessionCapture(browserCtx, capture)
		if err := newGarminBrowserMutationSession(browserCtx).discoverBase(); err != nil {
			return err
		}
		location, err := browserLocation(browserCtx)
		if err != nil {
			return err
		}
		if !isGarminConnectAppLocation(location) {
			return authErr(fmt.Errorf("Garmin redirected away from Workouts after verification"))
		}
		captured, ok, err := currentCapturedSession(browserCtx, capture)
		if err != nil {
			return err
		}
		if !ok || !sessionCandidateActive(captured) {
			return authErr(fmt.Errorf("no reusable Garmin session was captured"))
		}
		captured.VerifiedAt = time.Now()
		session = captured
		return nil
	})
	return session, err
}

func verifyGarminBrowserProfile(parent context.Context, profileDir string, timeout time.Duration) (garminsession.Session, error) {
	return verifyGarminBrowserProfileWithAction(parent, profileDir, timeout, nil)
}

func verifyGarminBrowserProfileWithAction(parent context.Context, profileDir string, timeout time.Duration, action func(context.Context) error) (garminsession.Session, error) {
	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.UserDataDir(profileDir),
		chromedp.WindowSize(1280, 900),
	}
	opts = append(opts, garminBrowserExecOptions()...)
	allocCtx, allocCancel := chromedp.NewExecAllocator(parent, opts...)
	defer allocCancel()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, timeoutCancel := context.WithTimeout(ctx, timeout)
	defer timeoutCancel()
	defer closeGarminBrowser(ctx)

	capture := &webSessionCapture{}
	startGarminSessionCapture(ctx, capture)

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate("https://connect.garmin.com/app/workouts"),
	); err != nil {
		return garminsession.Session{}, fmt.Errorf("opening Garmin Connect in the browser: %w", garminBrowserProfileError(err))
	}

	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	lastLocation := ""
	nextProbe := time.Time{}
	var lastProbeErr error
	for {
		select {
		case <-ctx.Done():
			if lastProbeErr != nil {
				return garminsession.Session{}, fmt.Errorf("timed out waiting for an authenticated Garmin workout session: %w", lastProbeErr)
			}
			return garminsession.Session{}, fmt.Errorf("timed out waiting for Garmin browser login; sign in and complete MFA in the browser")
		case <-ticker.C:
			location, err := browserLocation(ctx)
			if err == nil && location != "" {
				lastLocation = location
			} else if err != nil && !isTransientChromeContextError(err) {
				return garminsession.Session{}, err
			}
			if !isGarminConnectAppLocation(lastLocation) {
				continue
			}
			if time.Now().Before(nextProbe) {
				continue
			}
			nextProbe = time.Now().Add(10 * time.Second)
			probeErr := chromedp.Run(ctx, chromedp.ActionFunc(func(actionCtx context.Context) error {
				return newGarminBrowserMutationSession(actionCtx).discoverBase()
			}))
			if probeErr != nil {
				if ExitCode(probeErr) == 7 {
					return garminsession.Session{}, probeErr
				}
				lastProbeErr = probeErr
				continue
			}
			// The browser can briefly report the Garmin app URL before SSO redirects
			// away. Require a successful protected workout request and a final
			// authenticated-app location before persisting a session.
			location, err = browserLocation(ctx)
			if err != nil {
				if isTransientChromeContextError(err) {
					continue
				}
				return garminsession.Session{}, err
			}
			lastLocation = location
			if !isGarminConnectAppLocation(lastLocation) {
				lastProbeErr = fmt.Errorf("Garmin redirected the browser to %s after the workout probe", lastLocation)
				continue
			}
			session, ok, err := currentCapturedSession(ctx, capture)
			if err != nil {
				if isTransientChromeContextError(err) {
					continue
				}
				return garminsession.Session{}, err
			}
			if !ok || !sessionCandidateActive(session) {
				lastProbeErr = fmt.Errorf("Garmin workout API responded, but no authenticated browser session was captured")
				continue
			}
			session.VerifiedAt = time.Now()
			if action != nil {
				if err := chromedp.Run(ctx, chromedp.ActionFunc(action)); err != nil {
					return garminsession.Session{}, err
				}
			}
			return session, nil
		}
	}
}

func sessionCandidateActive(session garminsession.Session) bool {
	return session.Authorization != "" || session.Cookie != "" || len(session.Cookies) > 0
}

func isGarminConnectAppLocation(location string) bool {
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "connect.garmin.com" {
		return false
	}
	return strings.HasPrefix(parsed.Path, "/app/workouts") || strings.HasPrefix(parsed.Path, "/modern/workouts")
}

type webSessionCapture struct {
	mu             sync.Mutex
	authorization  string
	cookie         string
	userAgent      string
	seenAPI        bool
	garminRequests map[network.RequestID]struct{}
}

func startGarminSessionCapture(ctx context.Context, capture *webSessionCapture) {
	chromedp.ListenTarget(ctx, func(ev any) {
		switch event := ev.(type) {
		case *network.EventRequestWillBeSent:
			if !isGarminSessionRequest(event.Request.URL) {
				return
			}
			capture.mu.Lock()
			if capture.garminRequests == nil {
				capture.garminRequests = map[network.RequestID]struct{}{}
			}
			capture.garminRequests[event.RequestID] = struct{}{}
			capture.seenAPI = true
			capture.mu.Unlock()
			captureGarminSessionHeaders(capture, event.Request.Headers)
		case *network.EventRequestWillBeSentExtraInfo:
			capture.mu.Lock()
			_, isGarminRequest := capture.garminRequests[event.RequestID]
			capture.mu.Unlock()
			if isGarminRequest {
				captureGarminSessionHeaders(capture, event.Headers)
			}
		}
	})
}

func captureGarminSessionHeaders(capture *webSessionCapture, headers network.Headers) {
	auth := networkHeader(headers, "Authorization")
	cookie := networkHeader(headers, "Cookie")
	ua := networkHeader(headers, "User-Agent")
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if auth != "" {
		capture.authorization = auth
	}
	if cookie != "" {
		capture.cookie = cookie
	}
	if ua != "" {
		capture.userAgent = ua
	}
}

func browserLocation(ctx context.Context) (string, error) {
	var location string
	if err := chromedp.Run(ctx, chromedp.Location(&location)); err != nil {
		return "", fmt.Errorf("reading browser location: %w", err)
	}
	return location, nil
}

func currentCapturedSession(ctx context.Context, capture *webSessionCapture) (garminsession.Session, bool, error) {
	capture.mu.Lock()
	seenAPI := capture.seenAPI
	auth := capture.authorization
	capturedCookie := capture.cookie
	ua := capture.userAgent
	capture.mu.Unlock()
	if !seenAPI {
		return garminsession.Session{}, false, nil
	}
	if ua == "" {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`navigator.userAgent`, &ua)); err != nil {
			if auth == "" && !isTransientChromeContextError(err) {
				return garminsession.Session{}, false, fmt.Errorf("reading browser user agent: %w", err)
			}
		}
	}
	var cookies []*network.Cookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(actionCtx context.Context) error {
		var err error
		cookies, err = storage.GetCookies().Do(actionCtx)
		if err == nil && len(cookies) == 0 {
			cookies, err = network.GetCookies().Do(actionCtx)
		}
		return err
	}))
	if err != nil {
		if auth != "" || capturedCookie != "" {
			return garminsession.Session{
				Authorization: auth,
				Cookie:        capturedCookie,
				UserAgent:     ua,
				BaseURL:       garminsession.DefaultBaseURL(),
				CapturedAt:    time.Now(),
			}, true, nil
		}
		if isTransientChromeContextError(err) {
			return garminsession.Session{}, false, nil
		}
		return garminsession.Session{}, false, fmt.Errorf("reading Garmin cookies from the browser: %w", err)
	}
	cookie := cookieHeader(cookies)
	savedCookies := garminSessionCookies(cookies)
	if cookie == "" {
		cookie = capturedCookie
	}
	if auth == "" {
		storageAuth, err := browserStorageAuthorization(ctx)
		if err != nil {
			if isTransientChromeContextError(err) {
				if cookie == "" {
					return garminsession.Session{}, false, nil
				}
			} else {
				return garminsession.Session{}, false, err
			}
		} else if storageAuth != "" {
			auth = storageAuth
		} else if cookie == "" {
			return garminsession.Session{}, false, nil
		}
	}
	if auth == "" && cookie == "" {
		ok, err := browserViewerAuthenticated(ctx)
		if err != nil {
			if isTransientChromeContextError(err) {
				return garminsession.Session{}, false, nil
			}
			return garminsession.Session{}, false, err
		}
		if ok {
			return garminsession.Session{
				BaseURL:    garminsession.DefaultBaseURL(),
				CapturedAt: time.Now(),
			}, true, nil
		}
	}
	if auth == "" {
		if jwt := cliutil.FindJWTInCookieJar(cookie); jwt != "" {
			auth = "Bearer " + jwt
		}
	}
	return garminsession.Session{
		Authorization: auth,
		Cookie:        cookie,
		Cookies:       savedCookies,
		UserAgent:     ua,
		BaseURL:       garminsession.DefaultBaseURL(),
		CapturedAt:    time.Now(),
	}, true, nil
}

func garminSessionCookies(cookies []*network.Cookie) []garminsession.Cookie {
	saved := make([]garminsession.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(cookie.Domain)), ".")
		if cookie.Name == "" || cookie.Value == "" || (domain != "garmin.com" && !strings.HasSuffix(domain, ".garmin.com")) {
			continue
		}
		saved = append(saved, garminsession.Cookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Expires:  cookie.Expires,
			HTTPOnly: cookie.HTTPOnly,
			Secure:   cookie.Secure,
			Session:  cookie.Session,
			SameSite: string(cookie.SameSite),
		})
	}
	return saved
}

func browserStorageAuthorization(ctx context.Context) (string, error) {
	var rawValues string
	script := `JSON.stringify([
		...Object.keys(localStorage || {}).map((key) => localStorage.getItem(key) || ""),
		...Object.keys(sessionStorage || {}).map((key) => sessionStorage.getItem(key) || "")
	])`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &rawValues)); err != nil {
		return "", fmt.Errorf("reading Garmin browser storage: %w", err)
	}
	var values []string
	if err := json.Unmarshal([]byte(rawValues), &values); err != nil {
		return "", fmt.Errorf("parsing Garmin browser storage: %w", err)
	}
	for _, value := range values {
		if jwt := firstJWTInText(value); jwt != "" {
			return "Bearer " + jwt, nil
		}
	}
	return "", nil
}

func browserViewerAuthenticated(ctx context.Context) (bool, error) {
	var ok bool
	script := `Boolean(window.viewerIsAuthenticated) ||
		Boolean(document.documentElement && document.documentElement.classList.contains("signed-in")) ||
		Boolean(document.querySelector("meta[name='csrf-token']") && window.VIEWER_SOCIAL_PROFILE)`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &ok)); err != nil {
		return false, fmt.Errorf("checking Garmin browser sign-in state: %w", err)
	}
	return ok, nil
}

var jwtTextPattern = regexp.MustCompile(`[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}`)

func firstJWTInText(text string) string {
	for _, match := range jwtTextPattern.FindAllString(text, -1) {
		if cliutil.LooksLikeJWT(match) {
			return strings.TrimPrefix(match, "Bearer ")
		}
	}
	return ""
}

func isGarminSessionRequest(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "connect.garmin.com" && host != "connectapi.garmin.com" {
		return false
	}
	return strings.Contains(parsed.Path, "/workout-service/")
}

func networkHeader(headers network.Headers, name string) string {
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}

func cookieHeader(cookies []*network.Cookie) string {
	parts := make([]string, 0, len(cookies))
	seen := map[string]struct{}{}
	for _, cookie := range cookies {
		if cookie == nil || cookie.Name == "" || cookie.Value == "" {
			continue
		}
		if !strings.Contains(cookie.Domain, "garmin.com") {
			continue
		}
		key := cookie.Name + "=" + cookie.Value
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		parts = append(parts, key)
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

func isTransientChromeContextError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid context") || strings.Contains(msg, "context canceled")
}

func newAuthSetupCmd(flags *rootFlags) *cobra.Command {
	var launch bool
	cmd := &cobra.Command{
		Use:     "setup",
		Short:   "Get started with Garmin (use --launch to connect your account)",
		Example: "  garmin-connect-workout-cli auth setup\n  garmin-connect-workout-cli auth setup --launch",
		RunE: func(cmd *cobra.Command, args []string) error {
			if launch {
				login := newAuthLoginBrowserCmd(flags)
				return login.RunE(cmd, nil)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "1. Connect Garmin: garmin-connect-workout-cli auth setup --launch")
			fmt.Fprintln(w, "   Chrome opens only when sign-in or MFA is needed. Passwords stay in Garmin.")
			fmt.Fprintln(w, "   The window closes automatically; saved login is then verified headlessly.")
			fmt.Fprintln(w, "2. Optional recovery defaults: garmin-connect-workout-cli preferences setup")
			fmt.Fprintln(w, "3. Preview a workout: garmin-connect-workout-cli workouts plan \"2mi warmup, 6x400m at mile effort with full recovery\" --date YYYY-MM-DD")
			fmt.Fprintln(w, "4. Upload and schedule: garmin-connect-workout-cli workouts apply <draft-id> --apply")
			return nil
		},
	}
	cmd.Flags().BoolVar(&launch, "launch", false, "Connect Garmin, opening Chrome only if sign-in is needed")
	return cmd
}

func newAuthStatusCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Show the saved Garmin login (local check; use doctor --live to verify with Garmin)",
		Example: "  garmin-connect-workout-cli auth status",
		RunE: func(cmd *cobra.Command, args []string) error {
			session, sessionPath, found, err := garminsession.Load()
			if err != nil {
				return configErr(err)
			}
			profilePath, profileReady, err := garminsession.BrowserProfileReady()
			if err != nil {
				return configErr(err)
			}
			authed := profileReady && found && session.Active(time.Now())
			if flags.asJSON {
				out := map[string]any{
					"authenticated":   authed,
					"verified":        false,
					"browser_profile": map[string]any{"path": profilePath, "ready": profileReady},
				}
				if found {
					out["web_session_path"] = sessionPath
					out["web_session_active"] = session.Active(time.Now())
					out["web_session_captured_at"] = session.CapturedAt
				}
				if err := printJSONFiltered(cmd.OutOrStdout(), out, flags); err != nil {
					return err
				}
			} else if authed {
				fmt.Fprintf(cmd.OutOrStdout(), "Saved Garmin login present (not verified)\n  Web session: %s\n  Browser profile: %s\n", sessionPath, profilePath)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Not authenticated. Run: garmin-connect-workout-cli auth login-browser")
			}
			if !authed {
				return authErr(fmt.Errorf("no saved Garmin login"))
			}
			return nil
		},
	}
}

func newAuthLogoutCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "logout",
		Short:   "Clear the saved Garmin browser profile and session",
		Example: "  garmin-connect-workout-cli auth logout",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := garminsession.Clear(); err != nil {
				return configErr(fmt.Errorf("clearing Garmin browser session: %w", err))
			}
			if err := garminsession.ClearBrowserProfile(); err != nil {
				return configErr(fmt.Errorf("clearing Garmin browser profile: %w", err))
			}

			if flags.asJSON {
				return printJSONFiltered(cmd.OutOrStdout(), map[string]any{"cleared": true}, flags)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged out. Saved Garmin browser profile and session cleared.")
			return nil
		},
	}
}
