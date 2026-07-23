package workoutdraft

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"garmin-connect-workout-cli/internal/cliutil"
)

type Draft struct {
	ID              string         `json:"id"`
	Prompt          string         `json:"prompt"`
	Name            string         `json:"name"`
	Date            string         `json:"date,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	Workout         Workout        `json:"workout"`
	GarminPayload   map[string]any `json:"garmin_payload"`
	UploadedWorkout string         `json:"uploaded_workout_id,omitempty"`
	ScheduledID     string         `json:"scheduled_workout_id,omitempty"`
	ScheduledDate   string         `json:"scheduled_date,omitempty"`
	AppliedAt       *time.Time     `json:"applied_at,omitempty"`
}

type Workout struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Date     string   `json:"date,omitempty"`
	Duration int      `json:"duration_seconds"`
	Notes    []string `json:"notes,omitempty"`
	Steps    []Step   `json:"steps"`
}

type Step struct {
	Name             string  `json:"name"`
	StepType         string  `json:"step_type"`
	EndCondition     string  `json:"end_condition,omitempty"`
	DurationSec      int     `json:"duration_seconds,omitempty"`
	Distance         float64 `json:"distance,omitempty"`
	DistanceUOM      string  `json:"distance_unit,omitempty"`
	Target           string  `json:"target,omitempty"`
	Notes            string  `json:"notes,omitempty"`
	Repeat           int     `json:"repeat,omitempty"`
	SkipLastRecovery bool    `json:"skip_last_recovery,omitempty"`
	Steps            []Step  `json:"steps,omitempty"`
}

type Store struct {
	Path string
}

func NewStore() (Store, error) {
	dir, err := cliutil.DataDir()
	if err != nil {
		return Store{}, err
	}
	return Store{Path: filepath.Join(dir, "workout-drafts.json")}, nil
}

func Plan(prompt, date, name string) (Draft, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Draft{}, fmt.Errorf("workout prompt is required")
	}
	if date != "" {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return Draft{}, fmt.Errorf("--date must use YYYY-MM-DD: %w", err)
		}
	}
	steps, notes, err := parsePrompt(prompt)
	if err != nil {
		return Draft{}, err
	}
	if name == "" {
		name = inferName(prompt, date)
	}
	workout := Workout{Name: name, Type: "running", Date: date, Notes: notes, Steps: steps}
	payload, duration := GarminPayload(workout)
	workout.Duration = duration
	sum := sha1.Sum([]byte(prompt + "|" + date + "|" + name))
	id := "draft_" + hex.EncodeToString(sum[:])[:10]
	return Draft{
		ID:            id,
		Prompt:        prompt,
		Name:          name,
		Date:          date,
		CreatedAt:     time.Now().UTC(),
		Workout:       workout,
		GarminPayload: payload,
	}, nil
}

func (s Store) Save(d Draft) error {
	all, err := s.List()
	if err != nil {
		return err
	}
	replaced := false
	for i := range all {
		if all[i].ID == d.ID {
			all[i] = d
			replaced = true
			break
		}
	}
	if !replaced {
		all = append(all, d)
	}
	return s.write(all)
}

func (s Store) MarkApplied(id, workoutID, scheduledID, scheduleDate string) error {
	all, err := s.List()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for i := range all {
		if all[i].ID == id {
			all[i].UploadedWorkout = workoutID
			all[i].ScheduledID = scheduledID
			all[i].ScheduledDate = scheduleDate
			all[i].AppliedAt = &now
			return s.write(all)
		}
	}
	return fmt.Errorf("draft %q not found", id)
}

func (s Store) Get(id string) (Draft, error) {
	all, err := s.List()
	if err != nil {
		return Draft{}, err
	}
	for _, d := range all {
		if d.ID == id {
			return d, nil
		}
	}
	return Draft{}, fmt.Errorf("draft %q not found", id)
}

func (s Store) Search(query string) ([]Draft, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return all, nil
	}
	var out []Draft
	for _, d := range all {
		blob, _ := json.Marshal(d)
		if strings.Contains(strings.ToLower(string(blob)), query) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (s Store) List() ([]Draft, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Draft{}, nil
		}
		return nil, err
	}
	var drafts []Draft
	if err := json.Unmarshal(data, &drafts); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", s.Path, err)
	}
	return drafts, nil
}

func (s Store) write(drafts []Draft) error {
	data, err := json.MarshalIndent(drafts, "", "  ")
	if err != nil {
		return err
	}
	return cliutil.AtomicWritePrivateFile(s.Path, append(data, '\n'), 0o600, 0o700)
}

func parsePrompt(prompt string) ([]Step, []string, error) {
	parts := splitPromptParts(prompt)
	var steps []Step
	var notes []string
	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if step, ok := parseSetRepeat(raw); ok {
			steps = append(steps, step)
			continue
		}
		if step, ok := parseTimeRepeat(raw); ok {
			steps = append(steps, step)
			continue
		}
		if step, ok := parseRepeat(raw); ok {
			steps = append(steps, step)
			continue
		}
		if applyManualRecovery(&steps, raw) {
			continue
		}
		if step, ok := parseSingle(raw); ok {
			steps = append(steps, step)
			continue
		}
		notes = append(notes, raw)
	}
	if len(steps) == 0 {
		return nil, nil, fmt.Errorf("no workout steps parsed; use patterns like '35 min easy', '4x20s strides', '6x800m at 5K pace with 2 min jog', or '10 min cooldown'")
	}
	return steps, notes, nil
}

func splitPromptParts(prompt string) []string {
	var parts []string
	start := 0
	depth := 0
	for i, r := range prompt {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',', ';', '+':
			if depth == 0 {
				parts = append(parts, prompt[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, prompt[start:])

	var out []string
	then := regexp.MustCompile(`(?i)\bthen\b`)
	for _, part := range parts {
		out = append(out, then.Split(part, -1)...)
	}
	return out
}

func parseSetRepeat(s string) (Step, bool) {
	re := regexp.MustCompile(`(?i)^(\d+)\s+sets?\s+of\s*\((.+)\)(?:\s+with\s+(.+))?$`)
	m := re.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Step{}, false
	}
	repeat, _ := strconv.Atoi(m[1])
	children, notes, err := parsePrompt(m[2])
	if err != nil || len(notes) != 0 || len(children) == 0 {
		return Step{}, false
	}
	recoveryText := strings.TrimSpace(m[3])
	recoveryText = regexp.MustCompile(`(?i)\s+between\s+sets?$`).ReplaceAllString(recoveryText, "")
	if recovery, ok := parseRecovery(recoveryText); ok {
		children = append(children, recovery)
	}
	return Step{
		Name:             fmt.Sprintf("%d sets", repeat),
		StepType:         "repeat",
		Repeat:           repeat,
		SkipLastRecovery: len(children) > 0 && children[len(children)-1].StepType == "recovery",
		Steps:            children,
		Notes:            strings.TrimSpace(s),
	}, true
}

func parseRepeat(s string) (Step, bool) {
	re := regexp.MustCompile(`(?i)(\d+)\s*x\s*(\d+(?:\.\d+)?)\s*(km|k|m|mi|mile|miles)(?:\s+(?:at\s+)?(.+?))?(?:\s+with\s+(.+))?$`)
	m := re.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Step{}, false
	}
	repeat, _ := strconv.Atoi(m[1])
	dist, _ := strconv.ParseFloat(m[2], 64)
	unit := normalizeDistanceUnit(m[3])
	target := strings.TrimSpace(m[4])
	recoveryText := strings.TrimSpace(m[5])
	if recoveryText == "" {
		target, recoveryText = splitUntimedFullRecovery(target)
	}
	interval := Step{
		Name:        fmt.Sprintf("%s%s", trimFloat(dist), unit),
		StepType:    "interval",
		Distance:    dist,
		DistanceUOM: unit,
		Target:      target,
		Notes:       strings.TrimSpace(s),
	}
	children := []Step{interval}
	skipLastRecovery := false
	if recovery, ok := parseRecovery(recoveryText); ok {
		children = append(children, recovery)
	} else if recovery, ok := parseManualRecovery(recoveryText); ok {
		children = append(children, recovery)
		skipLastRecovery = true
	}
	return Step{
		Name:             fmt.Sprintf("%dx%s%s", repeat, trimFloat(dist), unit),
		StepType:         "repeat",
		Repeat:           repeat,
		SkipLastRecovery: skipLastRecovery,
		Steps:            children,
		Notes:            strings.TrimSpace(s),
	}, true
}

func splitUntimedFullRecovery(target string) (string, string) {
	lower := strings.ToLower(target)
	for _, phrase := range []string{"full recovery", "full recover", "全恢复"} {
		if index := strings.LastIndex(lower, phrase); index >= 0 && strings.TrimSpace(target[index+len(phrase):]) == "" {
			return strings.TrimSpace(target[:index]), target[index:]
		}
	}
	return target, ""
}

func parseTimeRepeat(s string) (Step, bool) {
	re := regexp.MustCompile(`(?i)(\d+)\s*x\s*(\d+(?:\.\d+)?)\s*(?:s|sec|secs|second|seconds|")\s*(.*?)(?:\s+with\s+(.+))?$`)
	m := re.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Step{}, false
	}
	repeat, _ := strconv.Atoi(m[1])
	seconds, _ := strconv.ParseFloat(m[2], 64)
	label := strings.TrimSpace(m[3])
	recoveryText := strings.TrimSpace(m[4])
	if label == "" {
		label = "Interval"
	}
	lower := strings.ToLower(s)
	name := "Interval"
	switch {
	case strings.Contains(lower, "stride"):
		name = "Stride"
	case strings.Contains(lower, "hill"):
		name = "Hill Sprint"
	case strings.Contains(lower, "sprint"):
		name = "Sprint"
	}
	interval := Step{
		Name:        name,
		StepType:    "interval",
		DurationSec: int(math.Round(seconds)),
		Target:      label,
		Notes:       strings.TrimSpace(s),
	}
	children := []Step{interval}
	skipLastRecovery := false
	if recovery, ok := parseRecovery(recoveryText); ok {
		children = append(children, recovery)
	} else if recovery, ok := parseManualRecovery(recoveryText); ok {
		children = append(children, recovery)
		skipLastRecovery = true
	} else if recovery, ok := parseManualRecovery(s); ok {
		children = append(children, recovery)
		skipLastRecovery = true
	} else if recovery := defaultRecoveryForRepeat(lower); recovery.DurationSec > 0 {
		children = append(children, recovery)
	}
	return Step{
		Name:             fmt.Sprintf("%dx%ss %s", repeat, trimFloat(seconds), strings.ToLower(name)),
		StepType:         "repeat",
		Repeat:           repeat,
		SkipLastRecovery: skipLastRecovery,
		Steps:            children,
		Notes:            strings.TrimSpace(s),
	}, true
}

func parseManualRecovery(s string) (Step, bool) {
	if regexp.MustCompile(`(?i)\d+(?:\.\d+)?\s*(?:min|mins|minute|minutes|sec|secs|second|seconds)\s+(?:full\s+recover|全恢复)`).MatchString(s) {
		return Step{}, false
	}
	text := manualRecoveryText(s)
	if text == "" {
		return Step{}, false
	}
	if regexp.MustCompile(`(?i)\d+(?:\.\d+)?\s*(?:min|mins|minute|minutes|sec|secs|second|seconds|km|k|m|mi|mile|miles)\b`).MatchString(text) {
		return Step{}, false
	}
	return Step{Name: "Recovery", StepType: "recovery", EndCondition: "lap.button", Notes: text}, true
}

func manualRecoveryText(s string) string {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "full recover") || strings.Contains(lower, "全恢复") {
		return "Full recovery: press Lap when ready"
	}
	first := -1
	for _, phrase := range []string{"walk down", "jog down", "walk back", "jog back"} {
		if index := strings.Index(lower, phrase); index >= 0 && (first < 0 || index < first) {
			first = index
		}
	}
	if first < 0 {
		return ""
	}
	return strings.TrimSpace(s[first:])
}

func applyManualRecovery(steps *[]Step, text string) bool {
	if len(*steps) == 0 {
		return false
	}
	last := &(*steps)[len(*steps)-1]
	if last.StepType != "repeat" {
		return false
	}

	recovery, ok := Step{}, false
	if isRecoveryText(text) {
		recovery, ok = parseRecovery(text)
	}
	skipLastRecovery := false
	if !ok {
		recovery, ok = parseManualRecovery(text)
		skipLastRecovery = ok
	}
	if !ok {
		return false
	}
	for i := range last.Steps {
		if last.Steps[i].StepType == "recovery" {
			last.Steps[i] = recovery
			last.SkipLastRecovery = skipLastRecovery
			return true
		}
	}
	last.Steps = append(last.Steps, recovery)
	last.SkipLastRecovery = skipLastRecovery
	return true
}

func defaultRecoveryForRepeat(lower string) Step {
	switch {
	case strings.Contains(lower, "hill"):
		return Step{Name: "Recovery", StepType: "recovery", DurationSec: 90, Notes: "Hill sprint recovery, defaulted to 90 seconds"}
	case strings.Contains(lower, "stride"):
		return Step{Name: "Recovery", StepType: "recovery", DurationSec: 60, Notes: "Stride recovery, defaulted to 60 seconds"}
	default:
		return Step{}
	}
}

func isRecoveryText(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "recover") || strings.Contains(lower, "jog") || strings.Contains(lower, "rest")
}

func parseRecovery(s string) (Step, bool) {
	if s == "" {
		return Step{}, false
	}
	if step, ok := parseSingle(s); ok {
		step.StepType = "recovery"
		if !strings.Contains(strings.ToLower(step.Name), "recover") {
			step.Name = "Recovery"
		}
		return step, true
	}
	return Step{}, false
}

func parseSingle(s string) (Step, bool) {
	lower := strings.ToLower(s)
	if m := regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(min|mins|minute|minutes|sec|secs|second|seconds)\b`).FindStringSubmatch(s); m != nil {
		n, _ := strconv.ParseFloat(m[1], 64)
		sec := int(math.Round(n))
		if strings.HasPrefix(strings.ToLower(m[2]), "min") {
			sec = int(math.Round(n * 60))
		}
		stepType := "interval"
		name := "Run"
		switch {
		case strings.Contains(lower, "warm"):
			stepType, name = "warmup", "Warm Up"
		case strings.Contains(lower, "cool"):
			stepType, name = "cooldown", "Cool Down"
		case strings.Contains(lower, "recover") || strings.Contains(lower, "jog") || strings.Contains(lower, "rest") || strings.Contains(lower, "float"):
			stepType, name = "recovery", "Recovery"
		case isEasyText(lower):
			name = "Easy"
		case strings.Contains(lower, "tempo"):
			name = "Tempo"
		}
		return Step{Name: name, StepType: stepType, DurationSec: sec, Target: extractTarget(s), Notes: s}, true
	}
	if m := regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(km|k|m|mi|mile|miles)\b`).FindStringSubmatch(s); m != nil {
		n, _ := strconv.ParseFloat(m[1], 64)
		unit := normalizeDistanceUnit(m[2])
		stepType := "interval"
		if strings.Contains(lower, "recover") || strings.Contains(lower, "jog") || strings.Contains(lower, "rest") {
			stepType = "recovery"
		}
		return Step{Name: fmt.Sprintf("%s%s", trimFloat(n), unit), StepType: stepType, Distance: n, DistanceUOM: unit, Target: extractTarget(s), Notes: s}, true
	}
	return Step{}, false
}

func GarminPayload(w Workout) (map[string]any, int) {
	order := 1
	steps, duration := garminSteps(w.Steps, &order)
	estimatedDuration := any(duration)
	if hasLapButtonStep(w.Steps) {
		estimatedDuration = nil
		duration = 0
	}
	description := strings.TrimSpace(w.Name + " - generated from natural language")
	if len(w.Notes) > 0 {
		description = strings.TrimSpace(description + "\nNotes: " + strings.Join(w.Notes, "; "))
	}
	payload := map[string]any{
		"sportType":                 map[string]any{"sportTypeId": 1, "sportTypeKey": "running", "displayOrder": 1},
		"subSportType":              nil,
		"workoutName":               w.Name,
		"description":               description,
		"estimatedDistanceUnit":     map[string]any{"unitKey": nil},
		"workoutSegments":           []any{map[string]any{"segmentOrder": 1, "sportType": map[string]any{"sportTypeId": 1, "sportTypeKey": "running", "displayOrder": 1}, "workoutSteps": steps}},
		"avgTrainingSpeed":          nil,
		"estimatedDurationInSecs":   estimatedDuration,
		"estimatedDistanceInMeters": estimatedDistance(w.Steps),
		"estimateType":              nil,
	}
	return payload, duration
}

func hasLapButtonStep(steps []Step) bool {
	for _, step := range steps {
		if step.EndCondition == "lap.button" || hasLapButtonStep(step.Steps) {
			return true
		}
	}
	return false
}

func garminSteps(steps []Step, order *int) ([]any, int) {
	var out []any
	total := 0
	for _, step := range steps {
		if step.StepType == "repeat" {
			id := *order
			*order++
			child, childDur := garminSteps(step.Steps, order)
			group := map[string]any{
				"stepId":             id,
				"stepOrder":          id,
				"stepType":           stepType("repeat"),
				"numberOfIterations": step.Repeat,
				"smartRepeat":        false,
				"endCondition":       map[string]any{"conditionTypeId": 7, "conditionTypeKey": "iterations", "displayOrder": 7, "displayable": false},
				"type":               "RepeatGroupDTO",
				"workoutSteps":       child,
			}
			if step.SkipLastRecovery {
				group["skipLastRestStep"] = true
			}
			out = append(out, group)
			total += step.Repeat * childDur
			continue
		}
		id := *order
		*order++
		item := map[string]any{
			"stepId":        id,
			"stepOrder":     id,
			"stepType":      stepType(step.StepType),
			"type":          "ExecutableStepDTO",
			"description":   stepDescription(step),
			"stepAudioNote": nil,
			"targetType":    map[string]any{"workoutTargetTypeId": 1, "workoutTargetTypeKey": "no.target", "displayOrder": 1},
		}
		if step.EndCondition == "lap.button" {
			item["endCondition"] = map[string]any{"conditionTypeId": 1, "conditionTypeKey": "lap.button", "displayOrder": 1, "displayable": true}
		} else if step.Distance > 0 {
			meters := distanceMeters(step.Distance, step.DistanceUOM)
			item["endCondition"] = map[string]any{"conditionTypeId": 3, "conditionTypeKey": "distance", "displayOrder": 3, "displayable": true}
			item["endConditionValue"] = step.Distance
			item["preferredEndConditionUnit"] = preferredDistanceUnit(step.DistanceUOM)
			total += estimateDuration(step, meters)
		} else {
			item["endCondition"] = map[string]any{"conditionTypeId": 2, "conditionTypeKey": "time", "displayOrder": 2, "displayable": true}
			item["endConditionValue"] = step.DurationSec
			total += step.DurationSec
		}
		if one, two, ok := paceTarget(step.Target); ok {
			item["targetType"] = map[string]any{"workoutTargetTypeId": 6, "workoutTargetTypeKey": "pace.zone", "displayOrder": 6}
			item["targetValueOne"] = one
			item["targetValueTwo"] = two
			item["targetValueUnit"] = nil
		}
		out = append(out, item)
	}
	return out, total
}

func stepType(kind string) map[string]any {
	switch kind {
	case "warmup":
		return map[string]any{"stepTypeId": 1, "stepTypeKey": "warmup", "displayOrder": 1}
	case "cooldown":
		return map[string]any{"stepTypeId": 2, "stepTypeKey": "cooldown", "displayOrder": 2}
	case "recovery":
		return map[string]any{"stepTypeId": 4, "stepTypeKey": "recovery", "displayOrder": 4}
	case "repeat":
		return map[string]any{"stepTypeId": 6, "stepTypeKey": "repeat", "displayOrder": 6}
	default:
		return map[string]any{"stepTypeId": 3, "stepTypeKey": "interval", "displayOrder": 3}
	}
}

func stepDescription(step Step) string {
	desc := step.Notes
	if step.Target != "" && step.Target != "easy" && !strings.Contains(strings.ToLower(desc), strings.ToLower(step.Target)) {
		desc = strings.TrimSpace(desc + " at " + step.Target)
	}
	return desc
}

func paceTarget(target string) (float64, float64, bool) {
	m := regexp.MustCompile(`(?i)(\d{1,2}):(\d{2})\s*/?\s*(km|k|mile|mi)`).FindStringSubmatch(target)
	if m == nil {
		return 0, 0, false
	}
	mins, _ := strconv.Atoi(m[1])
	secs, _ := strconv.Atoi(m[2])
	total := float64(mins*60 + secs)
	if normalizeDistanceUnit(m[3]) == "mi" {
		total = total / 1.609344
	}
	fast := 1000 / (total - 10)
	slow := 1000 / (total + 10)
	return fast, slow, true
}

func estimateDuration(step Step, meters float64) int {
	if one, two, ok := paceTarget(step.Target); ok && one > 0 && two > 0 {
		return int(math.Round(meters * (2 / (one + two))))
	}
	return int(math.Round(meters * 0.36))
}

func estimatedDistance(steps []Step) float64 {
	var total float64
	for _, step := range steps {
		if step.StepType == "repeat" {
			total += float64(step.Repeat) * estimatedDistance(step.Steps)
			continue
		}
		total += distanceMeters(step.Distance, step.DistanceUOM)
	}
	return total
}

func distanceMeters(value float64, unit string) float64 {
	switch unit {
	case "km":
		return value * 1000
	case "mi":
		return value * 1609.344
	default:
		return value
	}
}

// preferredDistanceUnit selects the unit shown by Garmin's distance picker.
// endConditionValue is expressed in this unit; metres are only used internally
// for workout-level estimates and duration calculations.
func preferredDistanceUnit(unit string) map[string]any {
	switch normalizeDistanceUnit(unit) {
	case "km":
		return map[string]any{"unitKey": "kilometer", "factor": 1000.0}
	case "mi":
		return map[string]any{"unitKey": "mile", "factor": 1609.344}
	default:
		return map[string]any{"unitKey": "meter", "factor": 1.0}
	}
}

func normalizeDistanceUnit(unit string) string {
	switch strings.ToLower(unit) {
	case "k", "km":
		return "km"
	case "mi", "mile", "miles":
		return "mi"
	default:
		return "m"
	}
}

func extractTarget(s string) string {
	if m := regexp.MustCompile(`(?i)\bat\s+(.+)$`).FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	if isEasyText(strings.ToLower(s)) {
		return "easy"
	}
	return ""
}

func isEasyText(lower string) bool {
	return strings.Contains(lower, "easy") || regexp.MustCompile(`(^|[^a-z])e([^a-z]|$)`).MatchString(lower)
}

func inferName(prompt string, date string) string {
	pieces := inferredNamePieces(prompt)
	if len(pieces) > 0 {
		name := strings.Join(pieces, " + ")
		if dateLabel := displayDate(date); dateLabel != "" {
			return dateLabel + ": " + name
		}
		return name
	}
	if m := regexp.MustCompile(`(?i)(\d+\s*x\s*\d+(?:\.\d+)?\s*(?:m|k|km|mi|mile|miles))`).FindStringSubmatch(prompt); m != nil {
		name := "Run " + strings.ReplaceAll(strings.ToUpper(m[1]), " ", "")
		if dateLabel := displayDate(date); dateLabel != "" {
			return dateLabel + ": " + name
		}
		return name
	}
	if dateLabel := displayDate(date); dateLabel != "" {
		return dateLabel + ": Run Workout"
	}
	return "Run Workout"
}

func inferredNamePieces(prompt string) []string {
	lower := strings.ToLower(prompt)
	var pieces []string
	if m := regexp.MustCompile(`(?i)\b(\d+(?:\.\d+)?)\s*(?:min|mins|minute|minutes)\s*(?:easy|e)\b`).FindStringSubmatch(prompt); m != nil {
		pieces = append(pieces, trimNumberText(m[1])+"E")
	}
	if strings.Contains(lower, "drill") {
		pieces = append(pieces, "Drills")
	}
	if m := regexp.MustCompile(`(?i)(\d+)\s*x\s*(\d+(?:\.\d+)?)\s*(?:s|sec|secs|second|seconds|")\b?\s*([a-z ]*)`).FindStringSubmatch(prompt); m != nil {
		label := strings.ToLower(strings.TrimSpace(m[3]))
		switch {
		case strings.Contains(label, "hill"):
			label = "hill sprints"
		case strings.Contains(label, "stride"):
			label = "strides"
		case strings.Contains(label, "sprint"):
			label = "sprints"
		default:
			label = "intervals"
		}
		pieces = append(pieces, fmt.Sprintf("%sx%ss %s", m[1], trimNumberText(m[2]), label))
	}
	return pieces
}

func displayDate(date string) string {
	if date == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return ""
	}
	return t.Format("January 2")
}

func trimNumberText(v string) string {
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return v
	}
	return trimFloat(n)
}

func trimFloat(v float64) string {
	if math.Abs(v-math.Round(v)) < 0.000001 {
		return strconv.Itoa(int(math.Round(v)))
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}
