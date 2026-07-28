// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bufio"
	"bytes"
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
	"garmin-connect-workout-cli/internal/config"
	"garmin-connect-workout-cli/internal/garminsession"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
	garmin "github.com/llehouerou/go-garmin"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAuthCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication for Garmin Connect Workouts",
		RunE:  parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newAuthSetupCmd(flags))
	cmd.AddCommand(newAuthLoginCmd(flags))
	cmd.AddCommand(newAuthLoginBrowserCmd(flags))
	cmd.AddCommand(newAuthStatusCmd(flags))
	cmd.AddCommand(newAuthSetTokenCmd(flags))
	cmd.AddCommand(newAuthLogoutCmd(flags))

	return cmd
}

func newAuthLoginCmd(flags *rootFlags) *cobra.Command {
	var email string
	var passwordStdin bool
	var mfaCode string
	cmd := &cobra.Command{
		Use:     "login",
		Short:   "Login to Garmin Connect without storing the raw password",
		Example: "  garmin-connect-workout-cli auth login\n  printf '%s' \"$GARMIN_PASSWORD\" | garmin-connect-workout-cli auth login --email you@example.com --password-stdin --mfa-code 123456",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				if flags.noInput || flags.agent || flags.asJSON || !stdinIsTerminal() {
					return cmd.Help()
				}
			}
			if strings.TrimSpace(email) == "" {
				if flags.noInput || flags.agent || flags.asJSON || !stdinIsTerminal() || passwordStdin {
					return usageErr(fmt.Errorf("--email is required"))
				}
				promptedEmail, err := promptLine(cmd, "Garmin email: ")
				if err != nil {
					return err
				}
				email = strings.TrimSpace(promptedEmail)
			}
			if email == "" {
				return usageErr(fmt.Errorf("--email is required"))
			}
			password, err := readGarminPassword(cmd, flags, passwordStdin)
			if err != nil {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			fmt.Fprintln(cmd.ErrOrStderr(), "Submitting Garmin login...")
			gc := garmin.New(garmin.Options{
				MFAHandler: func() (string, error) {
					if mfaCode == "" {
						if flags.noInput || flags.agent || flags.asJSON || passwordStdin || !stdinIsTerminal() {
							return "", fmt.Errorf("MFA required; rerun with --mfa-code")
						}
						fmt.Fprintln(cmd.ErrOrStderr(), "Garmin requested MFA. Check your Garmin-approved method, then enter the code.")
						code, err := promptLine(cmd, "MFA code: ")
						if err != nil {
							return "", err
						}
						return strings.TrimSpace(code), nil
					}
					return mfaCode, nil
				},
			})
			if err := gc.Login(ctx, email, password); err != nil {
				return authErr(fmt.Errorf("garmin login failed: %w", err))
			}
			var session bytes.Buffer
			if err := gc.SaveSession(&session); err != nil {
				return configErr(fmt.Errorf("reading Garmin session: %w", err))
			}
			var token struct {
				AccessToken  string    `json:"oauth2_access_token"`
				RefreshToken string    `json:"oauth2_refresh_token"`
				Expiry       time.Time `json:"oauth2_expiry"`
			}
			if err := json.Unmarshal(session.Bytes(), &token); err != nil {
				return configErr(fmt.Errorf("parsing Garmin session: %w", err))
			}
			if token.AccessToken == "" {
				return authErr(fmt.Errorf("Garmin login succeeded but no OAuth2 access token was returned"))
			}
			cfg, err := config.Load(flags.configPath)
			if err != nil {
				return configErr(err)
			}
			cfg.AuthHeaderVal = ""
			if err := cfg.SaveTokens("", "", token.AccessToken, token.RefreshToken, token.Expiry); err != nil {
				return configErr(fmt.Errorf("saving Garmin session token: %w", err))
			}
			out := map[string]any{
				"authenticated": true,
				"config_path":   cfg.Path,
				"expires_at":    token.Expiry,
			}
			if flags.asJSON || flags.agent {
				return printJSONFiltered(cmd.OutOrStdout(), out, flags)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Garmin token saved to %s\n", credentialSavePath(cfg))
			if !token.Expiry.IsZero() {
				fmt.Fprintf(cmd.OutOrStdout(), "Expires: %s\n", token.Expiry.Format(time.RFC3339))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Garmin Connect email address")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "Read Garmin password from stdin for scripts")
	cmd.Flags().StringVar(&mfaCode, "mfa-code", "", "MFA code when Garmin requires one")
	return cmd
}

func newAuthLoginBrowserCmd(flags *rootFlags) *cobra.Command {
	var timeout time.Duration
	var profileDir string
	cmd := &cobra.Command{
		Use:   "login-browser",
		Short: "Login through Garmin Connect in a browser",
		Long:  "Opens a visible browser window. Sign in to Garmin Connect there; the CLI verifies the signed-in browser profile and saves a local Garmin web session for later workout writes.",
		Example: strings.Join([]string{
			"  garmin-connect-workout-cli auth login-browser",
			"  garmin-connect-workout-cli auth login-browser --timeout 5m",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.noInput || flags.agent || flags.asJSON {
				return usageErr(fmt.Errorf("auth login-browser requires an interactive terminal"))
			}
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

			fmt.Fprintln(cmd.ErrOrStderr(), "Opening a browser for Garmin Connect login.")
			fmt.Fprintln(cmd.ErrOrStderr(), "Sign in and complete MFA in the browser. Leave this terminal open.")
			session, err := verifyGarminBrowserProfile(cmd.Context(), profileDir, timeout)
			if err != nil {
				return authErr(err)
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
			fmt.Fprintln(cmd.OutOrStdout(), "Garmin browser profile is signed in.")
			fmt.Fprintln(cmd.OutOrStdout(), "Garmin web session saved for workout writes.")
			fmt.Fprintf(cmd.OutOrStdout(), "Browser profile: %s\n", profileDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Web session: %s\n", sessionPath)
			return nil
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Maximum time to wait for browser login")
	cmd.Flags().StringVar(&profileDir, "profile-dir", "", "Browser profile directory for Garmin login cookies")
	return cmd
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

	capture := &webSessionCapture{}
	startGarminSessionCapture(ctx, capture)

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate("https://connect.garmin.com/app/workouts"),
	); err != nil {
		return garminsession.Session{}, fmt.Errorf("opening Garmin Connect in the browser: %w", err)
	}

	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	lastLocation := ""
	for {
		select {
		case <-ctx.Done():
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
			markGarminPageActivity(lastLocation, capture)
			session, ok, err := currentCapturedSession(ctx, capture)
			if err != nil {
				if isTransientChromeContextError(err) {
					continue
				}
				return garminsession.Session{}, err
			}
			if ok && session.Active(time.Now()) {
				if action != nil {
					if err := chromedp.Run(ctx, chromedp.ActionFunc(action)); err != nil {
						return garminsession.Session{}, err
					}
				}
				return session, nil
			}
		}
	}
}

func isGarminConnectAppLocation(location string) bool {
	parsed, err := url.Parse(location)
	return err == nil && parsed.Scheme == "https" && parsed.Host == "connect.garmin.com" && strings.HasPrefix(parsed.Path, "/app/")
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

func markGarminSessionSeen(capture *webSessionCapture) {
	capture.mu.Lock()
	capture.seenAPI = true
	capture.mu.Unlock()
}

func captureGarminWebSession(parent context.Context, profileDir string, timeout time.Duration) (garminsession.Session, error) {
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

	capture := &webSessionCapture{}
	startGarminSessionCapture(ctx, capture)

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate("https://connect.garmin.com/modern/workouts"),
	); err != nil {
		return garminsession.Session{}, fmt.Errorf("opening Garmin Connect in the browser: %w", err)
	}

	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	nextStatus := time.Now()
	lastLocation := ""
	for {
		select {
		case <-ctx.Done():
			return garminsession.Session{}, fmt.Errorf("timed out waiting for Garmin browser login; sign in, open Workouts, and rerun auth login-browser")
		case <-ticker.C:
			location, err := browserLocation(ctx)
			if err != nil && !isTransientChromeContextError(err) {
				return garminsession.Session{}, err
			}
			if location != "" {
				lastLocation = location
			}
			if time.Now().After(nextStatus) {
				nextStatus = time.Now().Add(10 * time.Second)
				if lastLocation == "" {
					fmt.Fprintln(os.Stderr, "Waiting for Garmin browser page...")
				} else {
					fmt.Fprintf(os.Stderr, "Waiting for Garmin sign-in from browser page: %s\n", lastLocation)
				}
			}
			markGarminPageActivity(location, capture)
			session, ok, err := currentCapturedSession(ctx, capture)
			if err != nil {
				return garminsession.Session{}, err
			}
			if ok {
				return session, nil
			}
		}
	}
}

func browserLocation(ctx context.Context) (string, error) {
	var location string
	if err := chromedp.Run(ctx, chromedp.Location(&location)); err != nil {
		return "", fmt.Errorf("reading browser location: %w", err)
	}
	return location, nil
}

func markGarminPageActivity(location string, capture *webSessionCapture) {
	if !strings.Contains(location, "garmin.com") {
		return
	}
	markGarminSessionSeen(capture)
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
	cookies, err := storage.GetCookies().Do(ctx)
	if err == nil && len(cookies) == 0 {
		cookies, err = network.GetCookies().Do(ctx)
	}
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
	if cookie == "" {
		cookie = capturedCookie
	}
	if auth == "" && cookie == "" {
		storageAuth, err := browserStorageAuthorization(ctx)
		if err != nil {
			if isTransientChromeContextError(err) {
				return garminsession.Session{}, false, nil
			}
			return garminsession.Session{}, false, err
		}
		if storageAuth == "" {
			return garminsession.Session{}, false, nil
		}
		auth = storageAuth
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
		UserAgent:     ua,
		BaseURL:       garminsession.DefaultBaseURL(),
		CapturedAt:    time.Now(),
	}, true, nil
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
	return strings.Contains(rawURL, "garmin.com")
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

func readGarminPassword(cmd *cobra.Command, flags *rootFlags, passwordStdin bool) (string, error) {
	if passwordStdin {
		passwordBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading password from stdin: %w", err)
		}
		password := strings.TrimRight(string(passwordBytes), "\r\n")
		if password == "" {
			return "", usageErr(fmt.Errorf("password stdin was empty"))
		}
		return password, nil
	}
	if flags.noInput || flags.agent || flags.asJSON {
		return "", usageErr(fmt.Errorf("--password-stdin is required in non-interactive mode"))
	}
	if !stdinIsTerminal() {
		return "", usageErr(fmt.Errorf("--password-stdin is required when stdin is not a terminal"))
	}
	fmt.Fprint(cmd.ErrOrStderr(), "Garmin password (hidden): ")
	passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", fmt.Errorf("reading hidden password: %w", err)
	}
	password := strings.TrimRight(string(passwordBytes), "\r\n")
	if password == "" {
		return "", usageErr(fmt.Errorf("password was empty"))
	}
	return password, nil
}

func promptLine(cmd *cobra.Command, label string) (string, error) {
	fmt.Fprint(cmd.ErrOrStderr(), label)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// newAuthSetupCmd prints concrete steps for getting a credential. Side-effect
// rule: print by default, --launch opt-in to open the URL, short-circuit when
// the verifier is running this in a sandboxed subprocess.
func newAuthSetupCmd(_ *rootFlags) *cobra.Command {
	var launch bool
	cmd := &cobra.Command{
		Use:     "setup",
		Short:   "Print steps for obtaining a credential (use --launch to open the URL)",
		Example: "  garmin-connect-workout-cli auth setup\n  garmin-connect-workout-cli auth setup --launch",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "Use browser login for Garmin Connect.")
			fmt.Fprintln(w, "")
			fmt.Fprintln(w, "Run:")
			fmt.Fprintln(w, "  garmin-connect-workout-cli auth login-browser")
			fmt.Fprintln(w, "")
			fmt.Fprintln(w, "That opens a browser so Garmin handles password and MFA directly.")
			if !launch {
				return nil
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "no setup URL configured; cannot launch")
			return nil
		},
	}
	cmd.Flags().BoolVar(&launch, "launch", false, "Open the setup URL in your default browser")
	return cmd
}

func newAuthStatusCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Show authentication status",
		Example: "  garmin-connect-workout-cli auth status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(flags.configPath)
			if err != nil {
				return configErr(err)
			}
			webSession, webSessionPath, webSessionFound, err := garminsession.Apply(cfg)
			if err != nil {
				return configErr(err)
			}
			browserProfilePath, browserProfileReady, err := garminsession.BrowserProfileReady()
			if err != nil {
				return configErr(err)
			}

			w := cmd.OutOrStdout()
			header := cfg.AuthHeader()
			authed := header != "" || (webSessionFound && webSession.Active(time.Now())) || browserProfileReady
			// JSON envelope: {authenticated, verified, source, config}. When not
			// authenticated, write the envelope first then return authErr
			// so exit code carries the auth-failure signal.
			if flags.asJSON {
				out := map[string]any{
					"authenticated": authed,
					"verified":      false,
					"source":        cfg.AuthSource,
					"config":        cfg.Path,
					"browser_profile": map[string]any{
						"path":  browserProfilePath,
						"ready": browserProfileReady,
					},
				}
				if webSessionFound {
					out["web_session_path"] = webSessionPath
					out["web_session_active"] = webSession.Active(time.Now())
					out["web_session_base_url"] = webSession.BaseURL
					out["web_session_captured_at"] = webSession.CapturedAt
				}
				if printErr := printJSONFiltered(w, out, flags); printErr != nil {
					return printErr
				}
				if !authed {
					return authErr(fmt.Errorf("no credentials configured"))
				}
				return nil
			}
			if !authed {
				fmt.Fprintln(w, red("Not authenticated"))
				fmt.Fprintln(w, "")
				fmt.Fprintln(w, "Login through Garmin Connect in a browser:")
				fmt.Fprintf(w, "  garmin-connect-workout-cli auth login-browser\n")
				return authErr(fmt.Errorf("no credentials configured"))
			}

			fmt.Fprintln(w, green("Credentials present (not verified)"))
			fmt.Fprintf(w, "  Source: %s\n", cfg.AuthSource)
			fmt.Fprintf(w, "  Config: %s\n", cfg.Path)
			if webSessionFound {
				fmt.Fprintf(w, "  Web session: %s\n", webSessionPath)
			}
			if browserProfileReady {
				fmt.Fprintf(w, "  Browser profile: %s\n", browserProfilePath)
				fmt.Fprintln(w, "  Browser profile is present; run `auth login-browser` again if Garmin rejects a write.")
			}
			return nil
		},
	}
}

func newAuthSetTokenCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "set-token <token>",
		Short:   "Save an API token to the credentials file",
		Example: "  garmin-connect-workout-cli auth set-token YOUR_TOKEN_HERE",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(flags.configPath)
			if err != nil {
				return configErr(err)
			}

			// Clear any legacy auth_header so AuthHeader() falls through to
			// the newly-saved credential. Without this, a pre-existing
			// auth_header value (common after regenerate) shadows the saved
			// token and set-token silently has no effect. Silent clear (no
			// log line): a masked-tail variant could leak token bytes through
			// scripted dogfood that captures stderr.
			cfg.AuthHeaderVal = ""
			if err := cfg.SaveTokens("", "", args[0], "", cfg.TokenExpiry); err != nil {
				return configErr(fmt.Errorf("saving token: %w", err))
			}

			savePath := credentialSavePath(cfg)
			// JSON envelope: {saved, config_path, credentials_path}.
			if flags.asJSON {
				out := map[string]any{
					"saved":       true,
					"config_path": cfg.Path,
				}
				if !cfg.AgentcookieManagedByExternalStore() {
					out["credentials_path"] = savePath
				}
				return printJSONFiltered(cmd.OutOrStdout(), out, flags)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Token saved to %s\n", savePath)
			return nil
		},
	}
}

func credentialSavePath(cfg *config.Config) string {
	if cfg != nil && cfg.AgentcookieManagedByExternalStore() {
		return cfg.Path
	}
	if path, err := cliutil.CredentialsFilePath(); err == nil {
		return path
	}
	if cfg != nil {
		return cfg.Path
	}
	return ""
}

func newAuthLogoutCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "logout",
		Short:   "Clear stored credentials",
		Example: "  garmin-connect-workout-cli auth logout",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(flags.configPath)
			if err != nil {
				return configErr(err)
			}

			if err := cfg.ClearTokens(); err != nil {
				return configErr(fmt.Errorf("clearing tokens: %w", err))
			}
			if err := garminsession.Clear(); err != nil {
				return configErr(fmt.Errorf("clearing Garmin browser session: %w", err))
			}
			if err := garminsession.ClearBrowserProfile(); err != nil {
				return configErr(fmt.Errorf("clearing Garmin browser profile: %w", err))
			}

			// Identify which (if any) auth env var is still exported so the
			// JSON envelope and the human prose can both surface it.
			envStillSet := ""
			if envStillSet == "" && os.Getenv("GARMIN_CONNECT_ACCESS_TOKEN") != "" {
				envStillSet = "GARMIN_CONNECT_ACCESS_TOKEN"
			}

			// JSON envelope: {cleared: true, note?: "<env_var> env var is still set"}.
			if flags.asJSON {
				out := map[string]any{"cleared": true}
				if envStillSet != "" {
					out["note"] = envStillSet + " env var is still set"
				}
				return printJSONFiltered(cmd.OutOrStdout(), out, flags)
			}

			if envStillSet != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Config cleared. Note: %s env var is still set.\n", envStillSet)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged out. Credentials cleared.")
			return nil
		},
	}
}
