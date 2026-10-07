// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"net/url"

	"github.com/spf13/cobra"
)

func garminBrowserReadPath(path string, params map[string]string) string {
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	if query := values.Encode(); query != "" {
		return path + "?" + query
	}
	return path
}

// newGarminReadCmd builds a read-only command that GETs one Garmin path
// through the saved browser session and prints the JSON response.
func newGarminReadCmd(flags *rootFlags, use, short, example, pathPrefix string) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Short:   short,
		Example: example,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := pathPrefix
			if len(args) == 1 {
				path += url.PathEscape(args[0])
			}
			return runGarminRead(cmd, flags, path, nil, 0)
		},
	}
}

// runGarminRead GETs path through the saved browser session and prints the JSON response.
func runGarminRead(cmd *cobra.Command, flags *rootFlags, path string, params map[string]string, limit int) error {
	data, _, err := garminBrowserGetJSON(cmd.Context(), garminBrowserReadPath(path, params))
	if err != nil {
		return classifyAPIError(err, flags)
	}
	return printOutputWithFlags(cmd.OutOrStdout(), truncateJSONArray(data, limit), flags)
}
