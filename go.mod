module garmin-connect-workout-cli

go 1.26

toolchain go1.26.4

require (
	github.com/pelletier/go-toml/v2 v2.2.4
	github.com/spf13/cobra v1.10.2
)

require modernc.org/sqlite v1.37.0

require (
	github.com/chromedp/cdproto v0.0.0-20260321001828-e3e3800016bc
	github.com/chromedp/chromedp v0.15.1
	github.com/llehouerou/go-garmin v0.0.0-20260217041215-cbf5895e08bf
	github.com/spf13/pflag v1.0.9
	golang.org/x/term v0.44.0
)

require (
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-json-experiment/json v0.0.0-20260214004413-d219187c3433 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/exp v0.0.0-20250305212735-054e65f0b394 // indirect
	golang.org/x/time v0.14.0 // indirect
	modernc.org/libc v1.62.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.9.1 // indirect
)

// Floor x/sys above the vulnerable v0.31.0. It is pulled only transitively
// (modernc.org/sqlite, golang.org/x/net, ...), so MVS needs this explicit
// floor; tidy drops it for CLIs that pull no x/sys at all.
require golang.org/x/sys v0.46.0 // indirect
