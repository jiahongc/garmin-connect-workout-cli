module garmin-connect-workout-cli

go 1.26

toolchain go1.26.4

require github.com/spf13/cobra v1.10.2

require (
	github.com/chromedp/cdproto v0.0.0-20260321001828-e3e3800016bc
	github.com/chromedp/chromedp v0.15.1
)

require (
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20260214004413-d219187c3433 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
)

// Floor x/sys above the vulnerable v0.31.0; it is pulled only transitively
// (chromedp -> gobwas/ws), so MVS needs this explicit floor.
require golang.org/x/sys v0.46.0 // indirect
