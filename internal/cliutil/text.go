// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cliutil

import (
	"regexp"
	"strings"
)

// LooksLikeAuthError checks if an error message body contains auth-related keywords.
func LooksLikeAuthError(msg string) bool {
	lower := strings.ToLower(msg)
	patterns := []string{
		`\bkey\b`,
		`\btoken\b`,
		`\bunauthorized\b`,
		`\bapi_key\b`,
		`missing.{0,20}key`,
		`required.{0,20}key`,
		`\bforbidden\b`,
		`\bauthenticat`,
		`\bcredential`,
	}
	for _, p := range patterns {
		if matched, _ := regexp.MatchString(p, lower); matched {
			return true
		}
	}
	return false
}
