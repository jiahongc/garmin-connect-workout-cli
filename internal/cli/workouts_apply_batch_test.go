// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"garmin-connect-workout-cli/internal/types"
	"garmin-connect-workout-cli/internal/workoutdraft"
)

func TestLoadWorkoutApplyBatchItemsPreservesDraftOrderAndDates(t *testing.T) {
	store := workoutdraft.Store{Path: filepath.Join(t.TempDir(), "drafts.json")}
	first := mustSaveBatchDraft(t, store, "First", "2026-07-28")
	second := mustSaveBatchDraft(t, store, "Second", "2026-08-04")

	items, err := loadWorkoutApplyBatchItems(store, []string{second.ID, first.ID}, false)
	if err != nil {
		t.Fatalf("loadWorkoutApplyBatchItems() error = %v", err)
	}
	if len(items) != 2 || items[0].Draft.ID != second.ID || items[0].Schedule != "2026-08-04" || items[1].Draft.ID != first.ID {
		t.Fatalf("items = %#v", items)
	}
}

func TestLoadWorkoutApplyBatchItemsRejectsDuplicateDrafts(t *testing.T) {
	store := workoutdraft.Store{Path: filepath.Join(t.TempDir(), "drafts.json")}
	draft := mustSaveBatchDraft(t, store, "Duplicate", "2026-07-28")
	if _, err := loadWorkoutApplyBatchItems(store, []string{draft.ID, draft.ID}, false); err == nil {
		t.Fatal("loadWorkoutApplyBatchItems() error = nil, want duplicate error")
	}
}

func TestWorkoutApplyBatchPreviewListsEveryDraftInRequestedOrder(t *testing.T) {
	store := workoutdraft.Store{Path: filepath.Join(t.TempDir(), "drafts.json")}
	first := mustSaveBatchDraft(t, store, "First", "2026-07-28")
	second := mustSaveBatchDraft(t, store, "Second", "2026-08-04")
	items, err := loadWorkoutApplyBatchItems(store, []string{second.ID, first.ID}, false)
	if err != nil {
		t.Fatalf("loadWorkoutApplyBatchItems() error = %v", err)
	}
	preview := workoutApplyBatchPreview(items, false, false, defaultGarminBatchMutationDelay)
	if preview["draft_count"] != 2 || preview["browser_sessions"] != 1 {
		t.Fatalf("preview = %#v", preview)
	}
	drafts, ok := preview["drafts"].([]map[string]any)
	if !ok || len(drafts) != 2 || drafts[0]["draft_id"] != second.ID || drafts[1]["draft_id"] != first.ID {
		t.Fatalf("preview drafts = %#v", preview["drafts"])
	}
}

func TestApplyWorkoutBatchItemCheckpointsUploadAndVerifiedSchedule(t *testing.T) {
	store := workoutdraft.Store{Path: filepath.Join(t.TempDir(), "drafts.json")}
	draft := mustSaveBatchDraft(t, store, "Workout A", "2026-07-28")
	item := workoutApplyBatchItem{Draft: draft, Schedule: draft.Date}
	livePayload, _ := json.Marshal(draft.GarminPayload)

	calendarReads := 0
	session := testGarminMutationSession(func(_ context.Context, base, method, path string, _ any) (browserPostResponse, error) {
		if method != "GET" {
			return browserPostResponse{}, fmt.Errorf("unexpected evaluator mutation")
		}
		if path == garminBrowserMutationProbePath {
			return browserPostResponse{BaseURL: base, Status: 200, Body: `[]`}, nil
		}
		if path == "/workout-service/workout/42" {
			return browserPostResponse{BaseURL: base, Status: 200, Body: string(livePayload)}, nil
		}
		calendarReads++
		if calendarReads == 1 {
			return browserPostResponse{BaseURL: base, Status: 200, Body: `{"calendarItems":[]}`}, nil
		}
		return browserPostResponse{
			BaseURL: base,
			Status:  200,
			Body:    `{"calendarItems":[{"id":99,"itemType":"workout","title":"Workout A","date":"2026-07-28","workoutId":42}]}`,
		}, nil
	})
	session.base = "a"

	var mutations []string
	mutate := func(method, path string, _ any, _ garminMutationVerifier) (browserPostResponse, error) {
		mutations = append(mutations, method+" "+path)
		if path == "/workout-service/workout" {
			return browserPostResponse{BaseURL: "a", Status: 200, Body: `{"workoutId":42}`}, nil
		}
		return browserPostResponse{BaseURL: "a", Status: 200, Body: `{}`}, nil
	}
	result, live, err := applyWorkoutBatchItem(
		context.Background(),
		&rootFlags{},
		store,
		session,
		mutate,
		item,
		nil,
		false,
	)
	if err != nil {
		t.Fatalf("applyWorkoutBatchItem() error = %v", err)
	}
	if len(mutations) != 2 || mutations[0] != "POST /workout-service/workout" || mutations[1] != "POST /workout-service/schedule/42" {
		t.Fatalf("mutations = %#v", mutations)
	}
	if len(live) != 1 || result["workout_id"] != "42" || result["scheduled_workout_id"] != "99" {
		t.Fatalf("result = %#v, live = %#v", result, live)
	}
	saved, err := store.Get(draft.ID)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if saved.UploadedWorkout != "42" || saved.ScheduledID != "99" || saved.ScheduledDate != "2026-07-28" || saved.AppliedAt == nil {
		t.Fatalf("saved draft = %#v", saved)
	}
}

func TestApplyWorkoutBatchItemRecovers427WithoutDuplicateUpload(t *testing.T) {
	store := workoutdraft.Store{Path: filepath.Join(t.TempDir(), "drafts.json")}
	draft := mustSaveBatchDraft(t, store, "Workout A", "2026-07-28")
	item := workoutApplyBatchItem{Draft: draft}
	livePayload, _ := json.Marshal(draft.GarminPayload)

	postCalls := 0
	session := testGarminMutationSession(func(_ context.Context, base, method, path string, _ any) (browserPostResponse, error) {
		if method == "POST" {
			postCalls++
			return browserPostResponse{BaseURL: base, Status: 427, Body: `{"error":{"status-code":"427"}}`}, nil
		}
		if path == garminBrowserMutationProbePath {
			return browserPostResponse{BaseURL: base, Status: 200, Body: `[]`}, nil
		}
		if path == "/workout-service/workout/42" {
			return browserPostResponse{BaseURL: base, Status: 200, Body: string(livePayload)}, nil
		}
		return browserPostResponse{
			BaseURL: base,
			Status:  200,
			Body:    `[{"workoutId":42,"workoutName":"Workout A"}]`,
		}, nil
	})
	session.base = "a"

	result, _, err := applyWorkoutBatchItem(
		context.Background(),
		&rootFlags{},
		store,
		session,
		session.mutate,
		item,
		nil,
		false,
	)
	if err != nil {
		t.Fatalf("applyWorkoutBatchItem() error = %v", err)
	}
	if postCalls != 1 {
		t.Fatalf("POST calls = %d, want 1", postCalls)
	}
	if result["workout_id"] != "42" || result["upload_status"] != "recovered" {
		t.Fatalf("result = %#v", result)
	}
}

func TestApplyWorkoutBatchItemRetries427OnlyAfterVerifiedAbsence(t *testing.T) {
	store := workoutdraft.Store{Path: filepath.Join(t.TempDir(), "drafts.json")}
	draft := mustSaveBatchDraft(t, store, "Workout A", "2026-07-28")
	item := workoutApplyBatchItem{Draft: draft}
	livePayload, _ := json.Marshal(draft.GarminPayload)

	var postBases []string
	absenceVerified := false
	created := false
	session := testGarminMutationSession(func(_ context.Context, base, method, path string, _ any) (browserPostResponse, error) {
		if method == "POST" {
			postBases = append(postBases, base)
			if base == "a" {
				return browserPostResponse{BaseURL: base, Status: 427, Body: `{"error":{"status-code":"427"}}`}, nil
			}
			if !absenceVerified {
				return browserPostResponse{}, fmt.Errorf("second POST happened before absence verification")
			}
			created = true
			return browserPostResponse{BaseURL: base, Status: 200, Body: `{"workoutId":42}`}, nil
		}
		if path == garminBrowserMutationProbePath {
			return browserPostResponse{BaseURL: base, Status: 200, Body: `[]`}, nil
		}
		if path == "/workout-service/workout/42" && created {
			return browserPostResponse{BaseURL: base, Status: 200, Body: string(livePayload)}, nil
		}
		absenceVerified = true
		return browserPostResponse{BaseURL: base, Status: 200, Body: `[]`}, nil
	})
	session.base = "a"

	result, _, err := applyWorkoutBatchItem(
		context.Background(),
		&rootFlags{},
		store,
		session,
		session.mutate,
		item,
		nil,
		false,
	)
	if err != nil {
		t.Fatalf("applyWorkoutBatchItem() error = %v", err)
	}
	if len(postBases) != 2 || postBases[0] != "a" || postBases[1] != "b" {
		t.Fatalf("POST bases = %#v, want [a b]", postBases)
	}
	if result["workout_id"] != "42" || result["upload_status"] != "created" {
		t.Fatalf("result = %#v", result)
	}
}

func TestApplyWorkoutBatchItemReplacesCheckpointedWorkoutAndVerifiesPayload(t *testing.T) {
	store := workoutdraft.Store{Path: filepath.Join(t.TempDir(), "drafts.json")}
	draft := mustSaveBatchDraft(t, store, "Workout A", "2026-07-28")
	if err := store.MarkApplied(draft.ID, "42", "99", draft.Date); err != nil {
		t.Fatalf("MarkApplied() error = %v", err)
	}
	draft, err := store.Get(draft.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	item := workoutApplyBatchItem{Draft: draft}

	livePayload, _ := json.Marshal(draft.GarminPayload)
	session := testGarminMutationSession(func(_ context.Context, base, method, path string, _ any) (browserPostResponse, error) {
		if method != "GET" || path != "/workout-service/workout/42" {
			return browserPostResponse{}, fmt.Errorf("unexpected evaluator request %s %s", method, path)
		}
		return browserPostResponse{BaseURL: base, Status: 200, Body: string(livePayload)}, nil
	})
	session.base = "a"

	var mutations []string
	mutate := func(method, path string, _ any, _ garminMutationVerifier) (browserPostResponse, error) {
		mutations = append(mutations, method+" "+path)
		return browserPostResponse{BaseURL: "a", Status: 200, Body: `{"workoutId":42}`}, nil
	}
	result, _, err := applyWorkoutBatchItem(
		context.Background(),
		&rootFlags{},
		store,
		session,
		mutate,
		item,
		[]types.Workout{{WorkoutId: "42", WorkoutName: "Workout A"}},
		true,
	)
	if err != nil {
		t.Fatalf("applyWorkoutBatchItem() error = %v", err)
	}
	if len(mutations) != 1 || mutations[0] != "PUT /workout-service/workout/42" {
		t.Fatalf("mutations = %#v", mutations)
	}
	if result["workout_id"] != "42" || result["upload_status"] != "updated" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExactGarminWorkoutNameMatches(t *testing.T) {
	workouts := []types.Workout{
		{WorkoutId: "1", WorkoutName: "A"},
		{WorkoutId: "2", WorkoutName: "AB"},
		{WorkoutId: "3", WorkoutName: "A"},
	}
	matches := exactGarminWorkoutNameMatches(workouts, "A")
	if len(matches) != 2 || matches[0].WorkoutId != "1" || matches[1].WorkoutId != "3" {
		t.Fatalf("matches = %#v", matches)
	}
}

func mustSaveBatchDraft(t *testing.T, store workoutdraft.Store, name, date string) workoutdraft.Draft {
	t.Helper()
	draft, err := workoutdraft.Plan("10 min easy", date, name)
	if err != nil {
		t.Fatalf("workoutdraft.Plan() error = %v", err)
	}
	draft.CreatedAt = time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	if err := store.Save(draft); err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}
	return draft
}
