package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type HarnessBundle struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Category       string `json:"category"`
	Description    string `json:"description"`
	DefaultEnabled bool   `json:"default_enabled,omitempty"`
}

type HarnessBundleList struct {
	Object string          `json:"object"`
	Data   []HarnessBundle `json:"data"`
}

type HarnessExperimentRequest struct {
	ExperimentID  string               `json:"experiment_id"`
	UserObjective HarnessUserObjective `json:"user_objective"`
	Bundles       []string             `json:"bundles"`
	SearchSpace   map[string]any       `json:"search_space,omitempty"`
	Execution     map[string]any       `json:"execution,omitempty"`
	Evolution     map[string]any       `json:"evolution,omitempty"`
}

type HarnessUserObjective struct {
	Primary     string         `json:"primary"`
	Constraints map[string]any `json:"constraints,omitempty"`
}

type HarnessExperimentList struct {
	Object string                     `json:"object"`
	Data   []HarnessExperimentSummary `json:"data"`
}

type HarnessExperimentSummary struct {
	ExperimentID string `json:"experiment_id"`
	State        string `json:"state"`
	Status       string `json:"status"`
	Progress     int    `json:"progress"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	Objective    string `json:"objective,omitempty"`
}

type HarnessExperimentDetail struct {
	ExperimentID string            `json:"experiment_id"`
	Object       string            `json:"object"`
	State        string            `json:"state"`
	Status       string            `json:"status"`
	Progress     int               `json:"progress"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
	Objective    string            `json:"objective,omitempty"`
	Bundles      []string          `json:"bundles,omitempty"`
	Artifacts    map[string]string `json:"artifacts,omitempty"`
	Summary      map[string]any    `json:"summary,omitempty"`
	Results      []map[string]any  `json:"results,omitempty"`
	Failures     []map[string]any  `json:"failures,omitempty"`
}

type harnessArtifact struct {
	contentType string
	body        []byte
}

type harnessExperimentState struct {
	spec      HarnessExperimentRequest
	state     string
	status    string
	progress  int
	createdAt time.Time
	updatedAt time.Time
	artifacts map[string]harnessArtifact
	summary   map[string]any
	results   []map[string]any
	failures  []map[string]any
}

type harnessRegistry struct {
	mu          sync.RWMutex
	experiments map[string]*harnessExperimentState
}

func newHarnessRegistry() *harnessRegistry {
	return &harnessRegistry{
		experiments: map[string]*harnessExperimentState{},
	}
}

func (r *harnessRegistry) create(spec HarnessExperimentRequest) (*harnessExperimentState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.experiments[spec.ExperimentID]; exists {
		return nil, false
	}

	now := time.Now().UTC()
	state := &harnessExperimentState{
		spec:      spec,
		state:     "queued",
		status:    "Experiment accepted",
		progress:  0,
		createdAt: now,
		updatedAt: now,
	}
	r.experiments[spec.ExperimentID] = state
	return cloneHarnessExperimentState(state), true
}

func (r *harnessRegistry) runStub(experimentID string) error {
	r.mu.Lock()
	state, exists := r.experiments[experimentID]
	if !exists {
		r.mu.Unlock()
		return fmt.Errorf("experiment %q was not found", experimentID)
	}
	state.state = "running"
	state.status = "Materializing stub artifacts"
	state.progress = 25
	state.updatedAt = time.Now().UTC()
	snapshot := cloneHarnessExperimentState(state)
	r.mu.Unlock()

	artifacts, summary, results, failures, err := buildHarnessStubArtifacts(snapshot)
	if err != nil {
		r.mu.Lock()
		if state, exists = r.experiments[experimentID]; exists {
			state.state = "failed"
			state.status = "Harness stub materialization failed"
			state.progress = 100
			state.updatedAt = time.Now().UTC()
			state.failures = []map[string]any{{
				"id":    "stub_failure_001",
				"stage": "materialization",
				"error": err.Error(),
			}}
		}
		r.mu.Unlock()
		return err
	}

	r.mu.Lock()
	if state, exists = r.experiments[experimentID]; exists {
		state.artifacts = artifacts
		state.summary = summary
		state.results = results
		state.failures = failures
		state.state = "completed"
		state.status = "Experiment completed (stub)"
		state.progress = 100
		state.updatedAt = time.Now().UTC()
	}
	r.mu.Unlock()
	return nil
}

func (r *harnessRegistry) list() []HarnessExperimentSummary {
	r.mu.RLock()
	states := make([]*harnessExperimentState, 0, len(r.experiments))
	for _, state := range r.experiments {
		states = append(states, cloneHarnessExperimentState(state))
	}
	r.mu.RUnlock()

	sort.SliceStable(states, func(i, j int) bool {
		return states[i].createdAt.After(states[j].createdAt)
	})

	result := make([]HarnessExperimentSummary, 0, len(states))
	for _, state := range states {
		result = append(result, state.summaryView())
	}
	return result
}

func (r *harnessRegistry) get(experimentID string) (*harnessExperimentState, bool) {
	r.mu.RLock()
	state, ok := r.experiments[experimentID]
	if !ok {
		r.mu.RUnlock()
		return nil, false
	}
	cloned := cloneHarnessExperimentState(state)
	r.mu.RUnlock()
	return cloned, true
}

func (r *harnessRegistry) getArtifact(experimentID, artifact string) (harnessArtifact, bool, bool) {
	r.mu.RLock()
	state, ok := r.experiments[experimentID]
	if !ok {
		r.mu.RUnlock()
		return harnessArtifact{}, false, false
	}
	materialized, artifactOK := state.artifacts[artifact]
	if !artifactOK {
		r.mu.RUnlock()
		return harnessArtifact{}, true, false
	}
	copied := cloneHarnessArtifact(materialized)
	r.mu.RUnlock()
	return copied, true, true
}

func cloneHarnessExperimentState(state *harnessExperimentState) *harnessExperimentState {
	if state == nil {
		return nil
	}

	cloned := *state
	cloned.spec.Bundles = append([]string(nil), state.spec.Bundles...)
	cloned.spec.SearchSpace = cloneMap(state.spec.SearchSpace)
	cloned.spec.Execution = cloneMap(state.spec.Execution)
	cloned.spec.Evolution = cloneMap(state.spec.Evolution)
	cloned.spec.UserObjective.Constraints = cloneMap(state.spec.UserObjective.Constraints)
	cloned.artifacts = cloneHarnessArtifactMap(state.artifacts)
	cloned.summary = cloneMap(state.summary)
	cloned.results = cloneResultList(state.results)
	cloned.failures = cloneResultList(state.failures)
	return &cloned
}

func cloneHarnessArtifactMap(source map[string]harnessArtifact) map[string]harnessArtifact {
	if source == nil {
		return nil
	}

	cloned := make(map[string]harnessArtifact, len(source))
	for key, value := range source {
		cloned[key] = cloneHarnessArtifact(value)
	}
	return cloned
}

func cloneHarnessArtifact(artifact harnessArtifact) harnessArtifact {
	return harnessArtifact{
		contentType: artifact.contentType,
		body:        append([]byte(nil), artifact.body...),
	}
}

func cloneResultList(source []map[string]any) []map[string]any {
	if source == nil {
		return nil
	}

	cloned := make([]map[string]any, 0, len(source))
	for _, item := range source {
		cloned = append(cloned, cloneMap(item))
	}
	return cloned
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}

	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneJSONValue(value)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneJSONValue(item)
		}
		return cloned
	default:
		return typed
	}
}

func (s *harnessExperimentState) summaryView() HarnessExperimentSummary {
	return HarnessExperimentSummary{
		ExperimentID: s.spec.ExperimentID,
		State:        s.state,
		Status:       s.status,
		Progress:     s.progress,
		CreatedAt:    s.createdAt.Format(time.RFC3339),
		UpdatedAt:    s.updatedAt.Format(time.RFC3339),
		Objective:    s.spec.UserObjective.Primary,
	}
}

func (s *harnessExperimentState) detail() HarnessExperimentDetail {
	return HarnessExperimentDetail{
		ExperimentID: s.spec.ExperimentID,
		Object:       "harness.experiment",
		State:        s.state,
		Status:       s.status,
		Progress:     s.progress,
		CreatedAt:    s.createdAt.Format(time.RFC3339),
		UpdatedAt:    s.updatedAt.Format(time.RFC3339),
		Objective:    s.spec.UserObjective.Primary,
		Bundles:      append([]string(nil), s.spec.Bundles...),
		Artifacts:    buildHarnessArtifactPaths(s.spec.ExperimentID),
		Summary:      cloneMap(s.summary),
		Results:      cloneResultList(s.results),
		Failures:     cloneResultList(s.failures),
	}
}

func (s server) handleHarnessBundles(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, HarnessBundleList{
		Object: "list",
		Data:   harnessBundleCatalog(),
	})
}

func (s server) handleHarnessCreateExperiment(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	var payload HarnessExperimentRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, APIError{
			Code:    "invalid_argument",
			Message: "request body must be valid JSON",
		})
		return
	}

	normalizeHarnessExperimentRequest(&payload)
	if err := validateHarnessExperimentRequest(payload); err != nil {
		writeError(writer, http.StatusBadRequest, APIError{
			Code:    "invalid_argument",
			Message: err.Error(),
		})
		return
	}

	state, created := s.harness.create(payload)
	if !created {
		writeError(writer, http.StatusConflict, APIError{
			Code:    "already_exists",
			Message: fmt.Sprintf("experiment %q already exists", payload.ExperimentID),
			Details: map[string]any{"experiment_id": payload.ExperimentID},
		})
		return
	}

	if err := s.harness.runStub(payload.ExperimentID); err != nil {
		writeError(writer, http.StatusInternalServerError, APIError{
			Code:    "experiment_failed",
			Message: fmt.Sprintf("failed to materialize stub artifacts for experiment %q", payload.ExperimentID),
			Details: map[string]any{"experiment_id": payload.ExperimentID},
		})
		return
	}

	writeJSON(writer, http.StatusAccepted, state.detail())
}

func (s server) handleHarnessListExperiments(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, HarnessExperimentList{
		Object: "list",
		Data:   s.harness.list(),
	})
}

func (s server) handleHarnessExperiment(writer http.ResponseWriter, request *http.Request) {
	experimentID := strings.TrimSpace(request.PathValue("experiment_id"))
	if experimentID == "" {
		writeError(writer, http.StatusBadRequest, APIError{
			Code:    "invalid_argument",
			Message: "experiment_id is required",
			Details: map[string]any{"field": "experiment_id"},
		})
		return
	}

	state, ok := s.harness.get(experimentID)
	if !ok {
		writeError(writer, http.StatusNotFound, APIError{
			Code:    "experiment_not_found",
			Message: fmt.Sprintf("experiment %q was not found", experimentID),
			Details: map[string]any{"experiment_id": experimentID},
		})
		return
	}

	writeJSON(writer, http.StatusOK, state.detail())
}

func (s server) handleHarnessExperimentArtifact(writer http.ResponseWriter, request *http.Request) {
	experimentID := strings.TrimSpace(request.PathValue("experiment_id"))
	artifact := strings.TrimSpace(request.PathValue("artifact"))
	if experimentID == "" {
		writeError(writer, http.StatusBadRequest, APIError{
			Code:    "invalid_argument",
			Message: "experiment_id is required",
			Details: map[string]any{"field": "experiment_id"},
		})
		return
	}
	if artifact == "" {
		writeError(writer, http.StatusBadRequest, APIError{
			Code:    "invalid_argument",
			Message: "artifact is required",
			Details: map[string]any{"field": "artifact"},
		})
		return
	}
	if !isHarnessArtifactKey(artifact) {
		writeError(writer, http.StatusNotFound, APIError{
			Code:    "artifact_not_found",
			Message: fmt.Sprintf("artifact %q is not available for experiment %q", artifact, experimentID),
			Details: map[string]any{"experiment_id": experimentID, "artifact": artifact},
		})
		return
	}

	materialized, experimentOK, artifactOK := s.harness.getArtifact(experimentID, artifact)
	if !experimentOK {
		writeError(writer, http.StatusNotFound, APIError{
			Code:    "experiment_not_found",
			Message: fmt.Sprintf("experiment %q was not found", experimentID),
			Details: map[string]any{"experiment_id": experimentID},
		})
		return
	}
	if !artifactOK {
		writeError(writer, http.StatusNotFound, APIError{
			Code:    "artifact_not_found",
			Message: fmt.Sprintf("artifact %q is not yet materialized for experiment %q", artifact, experimentID),
			Details: map[string]any{"experiment_id": experimentID, "artifact": artifact},
		})
		return
	}

	writer.Header().Set("Content-Type", materialized.contentType)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(materialized.body)
}

func normalizeHarnessExperimentRequest(request *HarnessExperimentRequest) {
	if request == nil {
		return
	}

	request.ExperimentID = strings.TrimSpace(request.ExperimentID)
	request.UserObjective.Primary = strings.TrimSpace(request.UserObjective.Primary)

	bundles := make([]string, 0, len(request.Bundles))
	for _, bundle := range request.Bundles {
		bundle = strings.TrimSpace(bundle)
		if bundle != "" {
			bundles = append(bundles, bundle)
		}
	}
	request.Bundles = bundles
}

func validateHarnessExperimentRequest(request HarnessExperimentRequest) error {
	if request.ExperimentID == "" {
		return fmt.Errorf("experiment_id is required")
	}
	if request.UserObjective.Primary == "" {
		return fmt.Errorf("user_objective.primary is required")
	}
	if len(request.Bundles) == 0 {
		return fmt.Errorf("at least one bundle is required")
	}
	return nil
}

func buildHarnessArtifactPaths(experimentID string) map[string]string {
	escapedID := url.PathEscape(strings.TrimSpace(experimentID))
	root := "/api/v1/harness/experiments/" + escapedID
	artifactsRoot := root + "/artifacts"
	return map[string]string{
		"status_path":       root,
		"plan_path":         artifactsRoot + "/plan",
		"summary_path":      artifactsRoot + "/summary",
		"promotion_path":    artifactsRoot + "/promotion",
		"pareto_front_path": artifactsRoot + "/pareto_front",
		"failures_path":     artifactsRoot + "/failures",
		"report_path":       artifactsRoot + "/report",
	}
}

func buildHarnessStubArtifacts(state *harnessExperimentState) (map[string]harnessArtifact, map[string]any, []map[string]any, []map[string]any, error) {
	candidateCount := estimateHarnessCandidateCount(state.spec.SearchSpace)
	if candidateCount < 1 {
		candidateCount = 1
	}

	representativeConfig := buildRepresentativeConfig(state.spec.SearchSpace)
	result := map[string]any{
		"id":            "cand_stub_0001",
		"score":         0.82,
		"quality_score": 0.82,
		"strict_pass":   true,
		"promotion":     "strict",
		"config":        representativeConfig,
	}
	results := []map[string]any{result}
	failures := []map[string]any{}
	summary := map[string]any{
		"object":                "harness.summary",
		"mode":                  "stub",
		"status":                "completed",
		"total_candidates":      candidateCount,
		"successful_candidates": 1,
		"failed_candidates":     0,
		"strict_promoted":       1,
		"rejected":              maxInt(candidateCount-1, 0),
		"duration_seconds":      0,
	}

	planPayload := map[string]any{
		"experiment_id":        state.spec.ExperimentID,
		"object":               "harness.plan",
		"mode":                 "stub",
		"status":               "materialized_stub",
		"objective":            state.spec.UserObjective.Primary,
		"bundles":              append([]string(nil), state.spec.Bundles...),
		"estimated_candidates": candidateCount,
		"search_space":         cloneMap(state.spec.SearchSpace),
		"selected_candidate":   representativeConfig,
	}
	promotionPayload := map[string]any{
		"experiment_id": state.spec.ExperimentID,
		"object":        "harness.promotion",
		"mode":          "stub",
		"candidate_id":  "cand_stub_0001",
		"promotion":     "strict",
		"score":         0.82,
		"config":        representativeConfig,
	}
	paretoFrontPayload := map[string]any{
		"object": "list",
		"data":   []map[string]any{result},
	}
	failuresPayload := map[string]any{
		"object": "list",
		"data":   failures,
	}
	reportBody := []byte(buildHarnessStubReport(state, candidateCount, representativeConfig))

	artifacts := map[string]harnessArtifact{}
	var err error
	if artifacts["plan"], err = marshalHarnessJSONArtifact(planPayload); err != nil {
		return nil, nil, nil, nil, err
	}
	if artifacts["summary"], err = marshalHarnessJSONArtifact(summary); err != nil {
		return nil, nil, nil, nil, err
	}
	if artifacts["promotion"], err = marshalHarnessJSONArtifact(promotionPayload); err != nil {
		return nil, nil, nil, nil, err
	}
	if artifacts["pareto_front"], err = marshalHarnessJSONArtifact(paretoFrontPayload); err != nil {
		return nil, nil, nil, nil, err
	}
	if artifacts["failures"], err = marshalHarnessJSONArtifact(failuresPayload); err != nil {
		return nil, nil, nil, nil, err
	}
	artifacts["report"] = harnessArtifact{
		contentType: "text/markdown; charset=utf-8",
		body:        reportBody,
	}

	return artifacts, summary, results, failures, nil
}

func marshalHarnessJSONArtifact(payload any) (harnessArtifact, error) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return harnessArtifact{}, err
	}
	body = append(body, '\n')
	return harnessArtifact{
		contentType: "application/json; charset=utf-8",
		body:        body,
	}, nil
}

func buildHarnessStubReport(state *harnessExperimentState, candidateCount int, representativeConfig map[string]any) string {
	return fmt.Sprintf(
		"# Harness Report\n\n"+
			"Experiment `%s` was materialized through the current stub runner.\n\n"+
			"## Status\n\n"+
			"- Mode: stub\n"+
			"- Objective: `%s`\n"+
			"- Estimated candidates: `%d`\n"+
			"- Bundles: `%s`\n\n"+
			"## Representative Candidate\n\n"+
			"```json\n%s\n```\n\n"+
			"This report is a placeholder artifact for the harness control plane. It does not yet represent real bundle evaluation or live runtime scoring.\n",
		state.spec.ExperimentID,
		state.spec.UserObjective.Primary,
		candidateCount,
		strings.Join(state.spec.Bundles, "`, `"),
		mustFormatJSON(representativeConfig),
	)
}

func mustFormatJSON(payload any) string {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(body)
}

func buildRepresentativeConfig(searchSpace map[string]any) map[string]any {
	if searchSpace == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(searchSpace))
	for key, value := range searchSpace {
		result[key] = selectRepresentativeValue(value)
	}
	return result
}

func selectRepresentativeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			result[key] = selectRepresentativeValue(nested)
		}
		return result
	case []any:
		if len(typed) == 0 {
			return []any{}
		}
		return selectRepresentativeValue(typed[0])
	default:
		return typed
	}
}

func estimateHarnessCandidateCount(searchSpace map[string]any) int {
	if len(searchSpace) == 0 {
		return 1
	}
	count := 1
	for _, value := range searchSpace {
		count *= estimateHarnessDimensionCount(value)
	}
	if count < 1 {
		return 1
	}
	return count
}

func estimateHarnessDimensionCount(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		count := 1
		for _, nested := range typed {
			count *= estimateHarnessDimensionCount(nested)
		}
		if count < 1 {
			return 1
		}
		return count
	case []any:
		if len(typed) == 0 {
			return 1
		}
		return len(typed)
	default:
		return 1
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func isHarnessArtifactKey(artifact string) bool {
	switch artifact {
	case "plan", "summary", "promotion", "pareto_front", "failures", "report":
		return true
	default:
		return false
	}
}

func harnessBundleCatalog() []HarnessBundle {
	return []HarnessBundle{
		{
			ID:             "core/exact_math",
			Name:           "Exact Math",
			Category:       "core",
			Description:    "Exact arithmetic gates such as 2+3=5 in zh/en.",
			DefaultEnabled: true,
		},
		{
			ID:             "core/plain_language",
			Name:           "Plain Language",
			Category:       "core",
			Description:    "Short natural-language generation and expression quality.",
			DefaultEnabled: true,
		},
		{
			ID:             "core/instruction_follow",
			Name:           "Instruction Follow",
			Category:       "core",
			Description:    "Hard-format and forced-constraint obedience checks.",
			DefaultEnabled: true,
		},
		{
			ID:          "memory/scope_recall",
			Name:        "Memory Scope Recall",
			Category:    "memory",
			Description: "Checks scoped memory retrieval and reuse behavior.",
		},
		{
			ID:          "runtime/latency_short",
			Name:        "Latency Short",
			Category:    "runtime",
			Description: "Measures short-form latency and time-to-first-token behavior.",
		},
	}
}
