// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/url"

	"garmin-connect-workout-cli/internal/client"
	"garmin-connect-workout-cli/internal/config"
)

func useGarminBrowserRead(cfg *config.Config) bool {
	return !hasGarminWriteAuth(cfg)
}

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

func resolveGarminWorkoutRead(ctx context.Context, c *client.Client, flags *rootFlags, isList bool, path string, params map[string]string, hintWriter io.Writer) (json.RawMessage, DataProvenance, error) {
	if flags.dataSource == "local" || !useGarminBrowserRead(c.Config) {
		return resolveReadWithStrategy(ctx, c, flags, "auto", "workouts", isList, path, params, nil, hintWriter)
	}
	data, _, err := garminBrowserGetJSON(ctx, garminBrowserReadPath(path, params))
	if err != nil {
		return nil, DataProvenance{}, err
	}
	writeThroughCache(ctx, "workouts", data)
	return data, attachFreshness(DataProvenance{Source: "live", Reason: "browser_session"}, flags), nil
}
