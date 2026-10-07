// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"garmin-connect-workout-cli/internal/cliutil"
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
)

var As = errors.As

func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		fi, err := f.Stat()
		if err != nil {
			return true
		}
		return (fi.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

type cliError struct {
	code int
	err  error
}

func (e *cliError) Error() string { return e.err.Error() }
func (e *cliError) Unwrap() error { return e.err }

func usageErr(err error) error     { return &cliError{code: 2, err: err} }
func notFoundErr(err error) error  { return &cliError{code: 3, err: err} }
func authErr(err error) error      { return &cliError{code: 4, err: err} }
func apiErr(err error) error       { return &cliError{code: 5, err: err} }
func configErr(err error) error    { return &cliError{code: 10, err: err} }
func rateLimitErr(err error) error { return &cliError{code: 7, err: err} }

// dryRunOK reports whether the command should short-circuit without doing any
// real work because --dry-run was set. The verify pipeline probes hand-written
// commands with --dry-run; commands that put validation in cobra's `Args:` or
// `MarkFlagRequired` cannot reach a dry-run guard inside RunE because cobra
// runs those checks before RunE. The verify-friendly pattern for hand-written
// commands is:
//
//	RunE: func(cmd *cobra.Command, args []string) error {
//	    if len(args) == 0 {
//	        return cmd.Help()
//	    }
//	    if dryRunOK(flags) {
//	        return nil
//	    }
//	    // ... real work ...
//	}
func dryRunOK(flags *rootFlags) bool {
	return flags != nil && flags.dryRun
}

// parentNoSubcommandRunE returns a RunE that handles parents invoked without a
// subcommand. In machine output (--json/--agent) the parent emits a structured
// error to stdout listing valid subcommands and exits 2; otherwise cobra's
// default help text is printed. Without this, agents driving the CLI in
// --agent mode received only human-readable help on stdout and exit 0, with no
// signal that the invocation was incomplete.
func parentNoSubcommandRunE(flags *rootFlags) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if flags != nil && flags.asJSON {
			subs := make([]string, 0, len(cmd.Commands()))
			for _, c := range cmd.Commands() {
				if c.IsAvailableCommand() && c.Name() != "help" {
					subs = append(subs, c.Name())
				}
			}
			sort.Strings(subs)
			_ = json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
				"error":             "subcommand required",
				"valid_subcommands": subs,
			})
			return usageErr(fmt.Errorf("subcommand required for %q", cmd.CommandPath()))
		}
		return cmd.Help()
	}
}

type noopResult struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func writeNoop(flags *rootFlags, reason, prose string) error {
	if flags != nil && flags.asJSON {
		return json.NewEncoder(os.Stdout).Encode(noopResult{Status: "noop", Reason: reason})
	}
	fmt.Fprintln(os.Stderr, prose)
	return nil
}

func writeAPIErrorEnvelope(flags *rootFlags, err error, code int) {
	if flags == nil || !flags.asJSON {
		return
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"error": err.Error(),
		"code":  code,
	})
}

// classifyAPIError maps API errors to structured exit codes with actionable hints.
func classifyAPIError(err error, flags *rootFlags) error {
	var typed *cliError
	if errors.As(err, &typed) {
		return err
	}

	msg := err.Error()
	switch {
	case strings.Contains(msg, "HTTP 409"):
		if flags != nil && flags.idempotent {
			return writeNoop(flags, "already_exists", "already exists (no-op)")
		}
		classified := apiErr(err)
		writeAPIErrorEnvelope(flags, classified, ExitCode(classified))
		return classified
	case strings.Contains(msg, "HTTP 401"), strings.Contains(msg, "HTTP 403"),
		strings.Contains(msg, "HTTP 400") && cliutil.LooksLikeAuthError(msg):
		return authErr(fmt.Errorf("%w\nhint: Garmin rejected the saved login; run 'garmin-connect-workout-cli auth login-browser'", err))
	case strings.Contains(msg, "HTTP 404"):
		return notFoundErr(fmt.Errorf("%w\nhint: resource not found. Run the 'list' command to see available items", err))
	case strings.Contains(msg, "HTTP 429"):
		return rateLimitErr(err)
	default:
		return apiErr(err)
	}
}

// classifyDeleteError maps DELETE errors and supports explicit idempotent no-op handling.
func classifyDeleteError(err error, flags *rootFlags) error {
	msg := err.Error()
	if strings.Contains(msg, "HTTP 404") && flags != nil && flags.ignoreMissing {
		return writeNoop(flags, "already_deleted", "already deleted (no-op)")
	}
	return classifyAPIError(err, flags)
}

// printJSONFiltered marshals a Go-typed value through the same output
// pipeline endpoint-mirror commands use. Hand-written novel commands that
// build a typed slice/struct call this so --select, --compact, --csv, and
// --quiet all behave the same way as on generator-emitted commands.
func printJSONFiltered(w io.Writer, v any, flags *rootFlags) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return printOutputWithFlags(w, json.RawMessage(raw), flags)
}

// filterFields keeps only the specified fields (comma-separated) from JSON objects/arrays.
// Supports dotted paths like "events.shortName" to descend into nested structures.
// Arrays are traversed element-wise: "events.shortName" keeps shortName on each event.
func filterFields(data json.RawMessage, fields string) json.RawMessage {
	var paths [][]string
	for _, f := range strings.Split(fields, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		parts := strings.Split(f, ".")
		for i := range parts {
			parts[i] = strings.ToLower(parts[i])
		}
		paths = append(paths, parts)
	}
	if len(paths) == 0 {
		return data
	}
	return filterFieldsRec(data, paths)
}

// filterFieldsRec applies path filters to a JSON value. Each path is a list of
// lowercase segments; arrays descend element-wise.
func filterFieldsRec(data json.RawMessage, paths [][]string) json.RawMessage {
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err == nil {
		out := make([]json.RawMessage, len(arr))
		for i, el := range arr {
			out[i] = filterFieldsRec(el, paths)
		}
		result, _ := json.Marshal(out)
		return result
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err == nil {
		keepWhole := map[string]bool{}
		subPaths := map[string][][]string{}
		for _, p := range paths {
			if len(p) == 0 {
				continue
			}
			head := p[0]
			if len(p) == 1 {
				keepWhole[head] = true
			} else {
				subPaths[head] = append(subPaths[head], p[1:])
			}
		}
		filtered := map[string]json.RawMessage{}
		matchedAny := false
		for k, v := range obj {
			matched := matchSelectSegment(k, keepWhole, subPaths)
			if matched == "" {
				continue
			}
			matchedAny = true
			if keepWhole[matched] {
				filtered[k] = v
				continue
			}
			if subs := subPaths[matched]; subs != nil {
				filtered[k] = filterFieldsRec(v, subs)
			}
		}
		// Envelope fallback: when no top-level keys matched but at least one
		// sibling is a non-null array, treat the object as a list envelope
		// (`{"items":[...]}`, `{"data":[...]}`, `{"total_count":N,"items":[...]}`)
		// and apply the selector inside the array(s). Non-array siblings pass
		// through verbatim so envelope metadata (counts, null pagination
		// cursors) stays visible. The foundArray guard preserves the prior
		// empty-object result for flat objects where no key matches and no
		// array exists. The `arr != nil` check rejects JSON null, which
		// json.Unmarshal otherwise accepts into a []json.RawMessage as a
		// nil slice and would coerce to `[]`.
		if !matchedAny {
			pending := map[string]json.RawMessage{}
			foundArray := false
			for k, v := range obj {
				var arr []json.RawMessage
				if json.Unmarshal(v, &arr) == nil && arr != nil {
					foundArray = true
					pending[k] = filterFieldsRec(v, paths)
				} else {
					pending[k] = v
				}
			}
			if foundArray {
				for k, v := range pending {
					filtered[k] = v
				}
			}
		}
		result, _ := json.Marshal(filtered)
		return result
	}

	return data
}

// matchSelectSegment returns the matching lowercase segment, or "" if no match.
// Supports direct case-insensitive match and camelCase→kebab-case conversion.
func matchSelectSegment(fieldName string, keepWhole map[string]bool, subPaths map[string][][]string) string {
	lower := strings.ToLower(fieldName)
	if keepWhole[lower] || subPaths[lower] != nil {
		return lower
	}
	kebab := camelToKebab(fieldName)
	if kebab != lower && (keepWhole[kebab] || subPaths[kebab] != nil) {
		return kebab
	}
	return ""
}

// camelToKebab converts "orderDate" or "orderdate" to "order-date" by splitting on
// uppercase boundaries. For already-lowercase input, splits on known word boundaries.
func camelToKebab(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) && unicode.IsLower(runes[i-1]) {
			b.WriteByte('-')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// printOutputWithFlags routes output through the right format based on flags.
func printOutputWithFlags(w io.Writer, data json.RawMessage, flags *rootFlags) error {
	// --select wins over --compact when both are set: an explicit field list
	// is the user's authoritative request, so the high-gravity allow-list
	// must not strip those fields out before --select can pick them. When
	// only --compact is set (e.g., --agent without --select), the allow-list
	// still runs.
	if flags.selectFields != "" {
		data = filterFields(data, flags.selectFields)
	} else if flags.compact {
		data = compactFields(data)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

// compactVerboseListFields are prose-shaped fields stripped from list-item
// projections. On lists, "body"/"content"/"html"/"markdown" are verbose
// noise and the row's identity is carried by id/name/title/etc.
var compactVerboseListFields = map[string]bool{
	"description": true, "body": true, "content": true,
	"comments": true, "attachments": true, "html": true, "markdown": true,
}

// compactVerboseObjectFields are metadata fields stripped from single-object
// responses. "body"/"content"/"html"/"markdown" are intentionally absent:
// for a `get` command those fields are the primary payload, and stripping
// them under `--agent`/`--compact` silently emits a useless envelope.
// Use `--select` to drop them explicitly.
var compactVerboseObjectFields = map[string]bool{
	"description": true,
	"comments":    true,
	"attachments": true,
}

// compactFields keeps only the most important fields for agent consumption.
// For arrays: allowlist of high-gravity fields (no descriptions).
// For single objects: blocklist that strips known-verbose fields (descriptions, comments, etc.).
func compactFields(data json.RawMessage) json.RawMessage {
	// Try array first
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err == nil {
		return compactListFields(items)
	}

	// Single object — use blocklist
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err == nil {
		return compactObjectFields(obj)
	}

	return data
}

// compactListFields keeps only high-gravity fields for array responses.
//
// Two-layer keep rule:
//
//  1. A static allow-list covers canonical scalars (id/name/price/status/...).
//  2. A data-driven extension also keeps any key present in at least 80% of
//     input rows. This catches hand-written novel commands whose payload keys
//     (object_name, match_key, snippet, series, metrics) aren't on the
//     canonical allow-list, without forcing every CLI to expand the
//     list.
//
// Verbose fields (description, body, content, etc.) are excluded from the
// data-driven extension regardless of frequency, so the compact intent
// (short identifying values for agent consumption, not full prose) is
// preserved.
//
// When an item still carries none of the keep keys, the original is
// preserved so `--agent` does not silently emit {} for shapes whose key
// names are entirely off-canonical.
func compactListFields(items []map[string]any) json.RawMessage {
	keepFields := map[string]bool{
		// Identity
		"id": true, "name": true, "title": true, "identifier": true,
		"code": true, "slug": true, "key": true,
		// Categorization
		"status": true, "state": true, "type": true, "kind": true, "priority": true,
		// Communication
		"url": true, "email": true,
		// Monetary
		"price": true, "amount": true, "cost": true, "fare": true,
		"rate": true, "currency": true,
		// Metrics
		"rating": true, "score": true, "count": true,
		// Locale / geo
		"language": true, "locale": true, "country": true, "region": true,
		"city": true, "domain": true,
		// Temporal
		"created_at": true, "updated_at": true, "createdAt": true, "updatedAt": true,
		"date": true,
		// Versioning
		"version": true,
	}
	if len(items) > 0 {
		keyCounts := map[string]int{}
		for _, item := range items {
			for k := range item {
				if compactVerboseListFields[k] {
					continue
				}
				keyCounts[k]++
			}
		}
		// ceil(len(items) * 0.8) without importing math. Capped at len-1 for
		// len >= 2 so a single missing row cannot veto a key on small lists
		// (without the cap, ceil(0.8*n) == n for n in {2,3,4}, which silently
		// reintroduces the partial-strip bug whenever a heterogeneous 2-4 row
		// response mixes one allow-list key with novel keys).
		threshold := (len(items)*4 + 4) / 5
		if len(items) >= 2 && threshold > len(items)-1 {
			threshold = len(items) - 1
		}
		for k, count := range keyCounts {
			if count >= threshold {
				keepFields[k] = true
			}
		}
	}

	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		compact := map[string]any{}
		for k, v := range item {
			if keepFields[k] {
				compact[k] = v
			}
		}
		if len(compact) == 0 {
			compact = item
		}
		filtered = append(filtered, compact)
	}
	result, _ := json.Marshal(filtered)
	return result
}

// compactObjectFields strips known-verbose metadata fields from single-object
// responses. The blocklist deliberately excludes "body"/"content"/"html"/
// "markdown" — those fields are payload on `get` commands and stripping them
// under `--agent`/`--compact` is a silent loss; agents who want to omit them
// can pass `--select` to specify only the fields they need.
func compactObjectFields(obj map[string]any) json.RawMessage {
	compact := map[string]any{}
	for k, v := range obj {
		if !compactVerboseObjectFields[k] {
			compact[k] = v
		}
	}
	result, _ := json.Marshal(compact)
	return result
}

// DataProvenance describes where data came from and when it was last synced.
type DataProvenance struct {
	Source       string     `json:"source"`                  // "live" or "local"
	SyncedAt     *time.Time `json:"synced_at,omitempty"`     // when local data was last synced
	Reason       string     `json:"reason,omitempty"`        // why local was used: "user_requested", "api_unreachable", "no_search_endpoint"
	ResourceType string     `json:"resource_type,omitempty"` // which resource type was queried
	Freshness    any        `json:"freshness,omitempty"`     // optional machine-owned freshness metadata for covered command paths
}

// truncateJSONArray returns a JSON array containing at most n elements
// from the input. When n <= 0, when the input isn't a JSON array, or
// when the array is already at-or-below the limit, the input is
// returned unchanged.
//
// Used by list-endpoint commands whose API ignores the ?limit=N query
// param (e.g. Firebase-style endpoints that return the full collection
// regardless of the param). The truncation is idempotent — calling it
// when the API already honored the limit is a no-op. The generator
// only emits the call when the spec declares a `limit` param without a
// Pagination block, so paginated APIs are unaffected.
func truncateJSONArray(data json.RawMessage, n int) json.RawMessage {
	if n <= 0 {
		return data
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		return data
	}
	if len(arr) <= n {
		return data
	}
	out, err := json.Marshal(arr[:n])
	if err != nil {
		return data
	}
	return out
}
