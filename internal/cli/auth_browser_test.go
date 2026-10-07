package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"garmin-connect-workout-cli/internal/cliutil"
	"garmin-connect-workout-cli/internal/garminsession"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func TestGarminSetupLaunchStartsLogin(t *testing.T) {
	restore, err := cliutil.SetHomeOverride(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	var output, diagnostics bytes.Buffer
	cmd := newAuthSetupCmd(&rootFlags{noInput: true, asJSON: true})
	cmd.SetOut(&output)
	cmd.SetErr(&diagnostics)
	if err := cmd.Flags().Set("launch", "true"); err != nil {
		t.Fatal(err)
	}
	err = cmd.RunE(cmd, nil)
	if ExitCode(err) != 4 || !strings.Contains(err.Error(), "sign-in is required") {
		t.Fatalf("setup should try login and report missing credentials: %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("setup must not mix instructions with JSON output: %q", output.String())
	}
}

func TestEnsureGarminBrowserSession(t *testing.T) {
	authRequired := authErr(fmt.Errorf("sign in required"))
	for _, tt := range []struct {
		name        string
		interactive bool
		errors      []error
		wantModes   []bool
		wantError   string
	}{
		{"saved login never opens Chrome", true, []error{nil}, []bool{true}, ""},
		{"new login verifies after closing Chrome", true, []error{authRequired, nil, nil}, []bool{true, false, true}, ""},
		{"agent can reuse saved login", false, []error{nil}, []bool{true}, ""},
		{"agent does not open a sign-in window", false, []error{authRequired}, []bool{true}, "without --agent"},
		{"rate limit does not trigger login", true, []error{rateLimitErr(fmt.Errorf("rate limited"))}, []bool{true}, "rate limited"},
		{"profile lock does not trigger login", true, []error{fmt.Errorf("profile locked")}, []bool{true}, "profile locked"},
		{"failed sign-in stops", true, []error{authRequired, fmt.Errorf("login cancelled")}, []bool{true, false}, "login cancelled"},
		{"unusable login is not success", true, []error{authRequired, nil, authRequired}, []bool{true, false, true}, "setup is not complete"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var modes []bool
			var output bytes.Buffer
			session, err := ensureGarminBrowserSession(&output, tt.interactive, func(headless bool) (garminsession.Session, error) {
				modes = append(modes, headless)
				if len(modes) > len(tt.errors) {
					t.Fatal("unexpected additional login attempt")
				}
				return garminsession.Session{Authorization: "synthetic"}, tt.errors[len(modes)-1]
			})
			if !reflect.DeepEqual(modes, tt.wantModes) {
				t.Fatalf("headless modes = %v, want %v", modes, tt.wantModes)
			}
			if tt.wantError == "" {
				if err != nil || session.Authorization != "synthetic" {
					t.Fatalf("successful session missing: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestGarminMissingSessionCookiesPreserveFreshProfile(t *testing.T) {
	session := garminsession.Session{Cookies: []garminsession.Cookie{
		{Name: "session", Value: "stale", Domain: ".garmin.com", Path: "/"},
		{Name: "missing", Value: "restore", Domain: ".garmin.com", Path: "/"},
		{Name: "session", Value: "other-path", Domain: ".garmin.com", Path: "/other"},
	}}
	current := []*network.Cookie{{Name: "session", Value: "fresh", Domain: ".garmin.com", Path: "/"}}
	got := garminMissingSessionCookieParams(session, current)
	if len(got) != 2 || got[0].Name != "missing" || got[1].Path != "/other" {
		t.Fatalf("restoration would overwrite fresh cookies: %+v", got)
	}
}

func TestGarminBrowserProfileSingletonErrors(t *testing.T) {
	for _, message := range []string{"Failed to create ProcessSingleton", "SingletonLock: File exists (17)"} {
		err := garminBrowserProfileError(fmt.Errorf("chrome failed to start: %s", message))
		if !strings.Contains(err.Error(), "Close only the dedicated Garmin Connect browser window") {
			t.Fatalf("missing actionable lock error: %v", err)
		}
	}
}

// Opt-in real Chrome regression, using only synthetic cookies in an isolated
// temporary profile. No Garmin requests or personal profile access.
func TestGarminCookieCaptureFromBrowserContext(t *testing.T) {
	if os.Getenv("GARMIN_CONNECT_BROWSER_TESTS") != "1" {
		t.Skip("set GARMIN_CONNECT_BROWSER_TESTS=1 to test real Chrome cookie capture")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, timeoutCancel := context.WithTimeout(ctx, 20*time.Second)
	defer timeoutCancel()
	defer closeGarminBrowser(ctx)
	if err := chromedp.Run(ctx, network.Enable(), chromedp.ActionFunc(func(actionCtx context.Context) error {
		return network.SetCookies([]*network.CookieParam{{Name: "test-session", Value: "synthetic-cookie", Domain: ".garmin.com", Path: "/", Secure: true}}).Do(actionCtx)
	})); err != nil {
		t.Fatal(err)
	}
	// This is deliberately outside ActionFunc, like the interactive login loop.
	session, ok, err := currentCapturedSession(ctx, &webSessionCapture{seenAPI: true, authorization: "synthetic-header", userAgent: "test"})
	if err != nil || !ok {
		t.Fatalf("capture failed: %v", err)
	}
	if len(session.Cookies) != 1 || session.Cookies[0].Value != "synthetic-cookie" {
		t.Fatalf("browser cookies were not captured; got %d cookies", len(session.Cookies))
	}
}
