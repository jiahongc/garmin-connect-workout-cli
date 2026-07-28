// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"fmt"
	"time"
)

const garminBrowserMutationProbePath = "/workout-service/workouts?start=0&limit=1"

const defaultGarminBrowserRequestDelay = 2 * time.Second

var garminBrowserMutationBases = []string{
	"https://connectapi.garmin.com",
	"/gc-api",
	"https://connect.garmin.com/proxy",
	"https://connect.garmin.com/modern/proxy",
}

type garminBrowserEvaluateFunc func(context.Context, string, string, string, any) (browserPostResponse, error)
type garminMutationVerifier func() (browserPostResponse, bool, error)

type garminBrowserMutationSession struct {
	ctx          context.Context
	bases        []string
	evaluate     garminBrowserEvaluateFunc
	onRateLimit  func(string) error
	onSuccess    func()
	requestDelay time.Duration
	lastRequest  time.Time
	now          func() time.Time
	wait         func(context.Context, time.Duration) error
	base         string
	blocked      map[string]bool
	circuitOpen  bool
}

func newGarminBrowserMutationSession(ctx context.Context) *garminBrowserMutationSession {
	return &garminBrowserMutationSession{
		ctx:      ctx,
		bases:    append([]string(nil), garminBrowserMutationBases...),
		evaluate: evaluateGarminBrowserRequest,
		onRateLimit: func(body string) error {
			return recordGarminMutationRateLimit(body, time.Now())
		},
		onSuccess: func() {
			_ = clearGarminMutationCircuit()
		},
		requestDelay: defaultGarminBrowserRequestDelay,
		now:          time.Now,
		wait:         waitForGarminMutationSpacing,
		blocked:      map[string]bool{},
	}
}

func (s *garminBrowserMutationSession) discoverBase() error {
	var firstFailure *browserPostResponse
	for _, base := range s.bases {
		if s.blocked[base] {
			continue
		}
		response, err := s.request(base, "GET", garminBrowserMutationProbePath, nil)
		if err != nil {
			return err
		}
		if response.Status == 429 {
			return s.openCircuit("GET", garminBrowserMutationProbePath, response)
		}
		if browserResponseOK(response) {
			s.base = base
			return nil
		}
		if response.Status == 427 || bodyLooksLikeHTML(response.Body) {
			s.blocked[base] = true
			continue
		}
		if firstFailure == nil {
			copy := response
			firstFailure = &copy
		}
	}
	if firstFailure != nil {
		return garminBrowserHTTPError("GET", garminBrowserMutationProbePath, *firstFailure)
	}
	return apiErr(fmt.Errorf("no authenticated Garmin workout route is available"))
}

func (s *garminBrowserMutationSession) read(path string) (browserPostResponse, error) {
	if s.circuitOpen {
		return browserPostResponse{}, rateLimitErr(fmt.Errorf("Garmin mutation circuit is open; no request was sent"))
	}
	for attempts := 0; attempts < len(s.bases); attempts++ {
		if s.base == "" || s.blocked[s.base] {
			s.base = ""
			if err := s.discoverBase(); err != nil {
				return browserPostResponse{}, err
			}
		}
		response, err := s.request(s.base, "GET", path, nil)
		if err != nil {
			return browserPostResponse{}, err
		}
		if response.Status == 429 {
			return response, s.openCircuit("GET", path, response)
		}
		if browserResponseOK(response) {
			return response, nil
		}
		if response.Status == 427 || bodyLooksLikeHTML(response.Body) {
			s.blocked[s.base] = true
			continue
		}
		return response, garminBrowserHTTPError("GET", path, response)
	}
	return browserPostResponse{}, apiErr(fmt.Errorf("all authenticated Garmin workout routes rejected GET %s", path))
}

func (s *garminBrowserMutationSession) mutate(method, path string, body any, verify garminMutationVerifier) (browserPostResponse, error) {
	if s.circuitOpen {
		return browserPostResponse{}, rateLimitErr(fmt.Errorf("Garmin mutation circuit is open; no request was sent"))
	}
	for attempts := 0; attempts < len(s.bases); attempts++ {
		if s.base == "" || s.blocked[s.base] {
			s.base = ""
			if err := s.discoverBase(); err != nil {
				return browserPostResponse{}, err
			}
		}
		response, err := s.request(s.base, method, path, body)
		if err != nil {
			return browserPostResponse{}, err
		}
		if response.Status == 429 {
			return response, s.openCircuit(method, path, response)
		}
		if browserResponseOK(response) {
			s.onSuccess()
			return response, nil
		}
		if response.Status != 427 {
			return response, garminBrowserHTTPError(method, path, response)
		}

		blockedBase := s.base
		s.blocked[blockedBase] = true
		s.base = ""
		if verify == nil {
			return response, apiErr(fmt.Errorf(
				"Garmin route %s rejected %s %s with HTTP 427; mutation was not retried because no duplicate-safety verifier was provided",
				blockedBase,
				method,
				path,
			))
		}
		verified, found, err := verify()
		if err != nil {
			return response, fmt.Errorf("verifying state after Garmin HTTP 427: %w", err)
		}
		if found {
			s.onSuccess()
			return verified, nil
		}
	}
	return browserPostResponse{}, apiErr(fmt.Errorf("all authenticated Garmin workout routes rejected %s %s with HTTP 427", method, path))
}

func (s *garminBrowserMutationSession) request(base, method, path string, body any) (browserPostResponse, error) {
	if !s.lastRequest.IsZero() {
		delay := s.lastRequest.Add(s.requestDelay).Sub(s.now())
		if delay > 0 {
			if err := s.wait(s.ctx, delay); err != nil {
				return browserPostResponse{}, err
			}
		}
	}
	response, err := s.evaluate(s.ctx, base, method, path, body)
	s.lastRequest = s.now()
	return response, err
}

func (s *garminBrowserMutationSession) openCircuit(method, path string, response browserPostResponse) error {
	s.circuitOpen = true
	recordErr := s.onRateLimit(response.Body)
	err := garminBrowserHTTPError(method, path, response)
	if recordErr != nil {
		return fmt.Errorf("%w; additionally failed to persist the local circuit breaker: %v", err, recordErr)
	}
	return err
}

func browserResponseOK(response browserPostResponse) bool {
	return response.Status >= 200 &&
		response.Status < 300 &&
		!bodyLooksLikeHTML(response.Body)
}
