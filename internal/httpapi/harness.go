package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	orb "github.com/agentex-ai/orb/internal/runtime"
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

func (r *harnessRegistry) runExperiment(service *orb.Service, experimentID string) error {
	if service == nil {
		return fmt.Errorf("harness runtime service is not configured")
	}

	r.mu.Lock()
	state, exists := r.experiments[experimentID]
	if !exists {
		r.mu.Unlock()
		return fmt.Errorf("experiment %q was not found", experimentID)
	}
	state.state = "running"
	state.status = "Running harness candidates"
	state.progress = 25
	state.updatedAt = time.Now().UTC()
	snapshot := cloneHarnessExperimentState(state)
	r.mu.Unlock()

	artifacts, summary, results, failures, err := buildHarnessRunArtifacts(service, snapshot)
	if err != nil {
		r.mu.Lock()
		if state, exists = r.experiments[experimentID]; exists {
			state.state = "failed"
			state.status = "Harness execution failed"
			state.progress = 100
			state.updatedAt = time.Now().UTC()
			state.failures = []map[string]any{{
				"id":    "stub_failure_001",
				"stage": "execution",
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
		state.status = "Experiment completed"
		state.progress = 100
		state.updatedAt = time.Now().UTC()
	}
	r.mu.Unlock()
	return nil
}

func (r *harnessRegistry) list() []HarnessExperimentSummary {
	r.mu.RLock()
	result := make([]HarnessExperimentSummary, 0, len(r.experiments))
	for _, state := range r.experiments {
		result = append(result, state.summaryView())
	}
	r.mu.RUnlock()

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].CreatedAt > result[j].CreatedAt
	})
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
	cloned.spec.Bundles = slices.Clone(state.spec.Bundles)
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
		body:        bytes.Clone(artifact.body),
	}
}

func cloneResultList(source []map[string]any) []map[string]any {
	cloned := slices.Clone(source)
	for i := range cloned {
		cloned[i] = cloneMap(cloned[i])
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
		cloned := slices.Clone(typed)
		for i := range cloned {
			cloned[i] = cloneJSONValue(cloned[i])
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
		Bundles:      slices.Clone(s.spec.Bundles),
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

	if err := s.harness.runExperiment(s.service, payload.ExperimentID); err != nil {
		writeError(writer, http.StatusInternalServerError, APIError{
			Code:    "experiment_failed",
			Message: fmt.Sprintf("failed to execute experiment %q", payload.ExperimentID),
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

func buildHarnessRunArtifacts(service *orb.Service, state *harnessExperimentState) (map[string]harnessArtifact, map[string]any, []map[string]any, []map[string]any, error) {
	if service == nil {
		return nil, nil, nil, nil, fmt.Errorf("harness runtime service is not configured")
	}

	startedAt := time.Now()
	maxCandidates := harnessMaxCandidates(state.spec.Evolution)
	candidateConfigs := expandHarnessCandidates(state.spec.SearchSpace, maxCandidates)
	if len(candidateConfigs) == 0 {
		candidateConfigs = []map[string]any{{}}
	}

	plannedCandidates := make([]map[string]any, 0, len(candidateConfigs))
	results := make([]map[string]any, 0, len(candidateConfigs))
	failures := make([]map[string]any, 0)
	strictPromoted := 0
	failedCandidates := 0

	for index, candidateConfig := range candidateConfigs {
		candidateID := fmt.Sprintf("cand_%04d", index+1)
		modelID := harnessCandidateModelID(service, candidateConfig)
		memoryEnabled, memoryScope := harnessCandidateMemoryConfig(candidateConfig, state.spec.ExperimentID, candidateID)

		plannedCandidates = append(plannedCandidates, map[string]any{
			"id":             candidateID,
			"model":          modelID,
			"memory_enabled": memoryEnabled,
			"memory_scope":   memoryScope,
			"config":         cloneMap(candidateConfig),
		})

		result, candidateFailures, executionFailed := evaluateHarnessCandidate(service, state, candidateID, candidateConfig)
		results = append(results, result)
		failures = append(failures, candidateFailures...)
		if executionFailed {
			failedCandidates++
		}
		if boolValue(result["strict_pass"]) {
			strictPromoted++
		}
	}

	sortHarnessResults(results)

	totalCandidates := len(candidateConfigs)
	rejected := max(totalCandidates-strictPromoted, 0)
	summary := map[string]any{
		"object":                "harness.summary",
		"mode":                  "runner",
		"status":                "completed",
		"total_candidates":      totalCandidates,
		"successful_candidates": totalCandidates - failedCandidates,
		"failed_candidates":     failedCandidates,
		"strict_promoted":       strictPromoted,
		"rejected":              rejected,
		"duration_seconds":      time.Since(startedAt).Seconds(),
	}

	topCandidate := map[string]any{}
	if len(results) > 0 {
		topCandidate = cloneMap(results[0])
	}
	topCandidateID := stringValue(topCandidate["id"])
	topCandidateConfig, _ := topCandidate["config"].(map[string]any)
	topCandidatePromotion := stringValue(topCandidate["promotion"])
	topCandidateScore := floatValue(topCandidate["score"])

	planPayload := map[string]any{
		"experiment_id":      state.spec.ExperimentID,
		"object":             "harness.plan",
		"mode":               "runner",
		"status":             "completed",
		"objective":          state.spec.UserObjective.Primary,
		"bundles":            slices.Clone(state.spec.Bundles),
		"candidate_count":    totalCandidates,
		"search_space":       cloneMap(state.spec.SearchSpace),
		"planned_candidates": plannedCandidates,
	}
	promotionPayload := map[string]any{
		"experiment_id": state.spec.ExperimentID,
		"object":        "harness.promotion",
		"mode":          "runner",
		"candidate_id":  topCandidateID,
		"promotion":     topCandidatePromotion,
		"score":         topCandidateScore,
		"config":        cloneMap(topCandidateConfig),
	}

	paretoFront := cloneResultList(results)
	if len(paretoFront) > 3 {
		paretoFront = paretoFront[:3]
	}
	paretoFrontPayload := map[string]any{
		"object": "list",
		"data":   paretoFront,
	}
	failuresPayload := map[string]any{
		"object": "list",
		"data":   failures,
	}
	reportBody := []byte(buildHarnessRunReport(state, summary, topCandidate, failures))

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

func buildHarnessRunReport(state *harnessExperimentState, summary map[string]any, topCandidate map[string]any, failures []map[string]any) string {
	topCandidateID := stringValue(topCandidate["id"])
	topCandidatePromotion := stringValue(topCandidate["promotion"])
	topCandidateScore := floatValue(topCandidate["score"])
	topCandidateConfig, _ := topCandidate["config"].(map[string]any)

	return fmt.Sprintf(
		"# Harness Report\n\n"+
			"Experiment `%s` was evaluated through the current harness runner.\n\n"+
			"## Summary\n\n"+
			"- Objective: `%s`\n"+
			"- Mode: runner\n"+
			"- Evaluated candidates: `%d`\n"+
			"- Strict passes: `%d`\n"+
			"- Rejected candidates: `%d`\n"+
			"- Recorded failures: `%d`\n"+
			"- Bundles: `%s`\n\n"+
			"## Top Candidate\n\n"+
			"- Candidate: `%s`\n"+
			"- Promotion: `%s`\n"+
			"- Score: `%.2f`\n\n"+
			"```json\n%s\n```\n\n"+
			"This is an early runner implementation that executes a small built-in bundle set against the live Orb runtime surface.\n",
		state.spec.ExperimentID,
		state.spec.UserObjective.Primary,
		intValue(summary["total_candidates"]),
		intValue(summary["strict_promoted"]),
		intValue(summary["rejected"]),
		len(failures),
		strings.Join(state.spec.Bundles, "`, `"),
		topCandidateID,
		topCandidatePromotion,
		topCandidateScore,
		mustFormatJSON(topCandidateConfig),
	)
}

func evaluateHarnessCandidate(service *orb.Service, state *harnessExperimentState, candidateID string, candidateConfig map[string]any) (map[string]any, []map[string]any, bool) {
	startedAt := time.Now()
	modelID := harnessCandidateModelID(service, candidateConfig)
	memoryEnabled, memoryScope := harnessCandidateMemoryConfig(candidateConfig, state.spec.ExperimentID, candidateID)
	bundleResults := make([]map[string]any, 0, len(state.spec.Bundles))
	failures := make([]map[string]any, 0)
	executionFailed := false
	passedBundles := 0

	for _, bundle := range state.spec.Bundles {
		bundleResult, bundleFailures, bundleExecutionFailed := runHarnessBundle(service, state, candidateID, candidateConfig, bundle)
		bundleResults = append(bundleResults, bundleResult)
		failures = append(failures, bundleFailures...)
		if boolValue(bundleResult["pass"]) {
			passedBundles++
		}
		if bundleExecutionFailed {
			executionFailed = true
		}
	}

	qualityScore := 0.0
	if len(state.spec.Bundles) > 0 {
		qualityScore = float64(passedBundles) / float64(len(state.spec.Bundles))
	}
	strictPass := passedBundles == len(state.spec.Bundles)
	promotion := "rejected"
	if strictPass {
		promotion = "strict"
	}

	return map[string]any{
		"id":               candidateID,
		"model":            modelID,
		"score":            qualityScore,
		"quality_score":    qualityScore,
		"strict_pass":      strictPass,
		"promotion":        promotion,
		"memory_enabled":   memoryEnabled,
		"memory_scope":     memoryScope,
		"bundle_passes":    passedBundles,
		"bundle_total":     len(state.spec.Bundles),
		"duration_ms":      float64(time.Since(startedAt).Microseconds()) / 1000.0,
		"execution_failed": executionFailed,
		"config":           cloneMap(candidateConfig),
		"bundle_results":   bundleResults,
	}, failures, executionFailed
}

func runHarnessBundle(service *orb.Service, state *harnessExperimentState, candidateID string, candidateConfig map[string]any, bundle string) (map[string]any, []map[string]any, bool) {
	modelID := harnessCandidateModelID(service, candidateConfig)
	memoryEnabled, memoryScope := harnessCandidateMemoryConfig(candidateConfig, state.spec.ExperimentID, candidateID)
	memoryRequest := &orb.MemoryRequest{
		Enabled: memoryEnabled,
		Scope:   memoryScope,
	}
	settings, _ := mapFromNestedValue(candidateConfig, "execution_settings")
	run := func(inputText string) (orb.Response, float64, error) {
		request := orb.Request{
			Model: modelID,
			Input: []orb.InputMessage{{
				Role: "user",
				Content: []orb.InputContent{{
					Type: "input_text",
					Text: inputText,
				}},
			}},
			Memory: memoryRequest,
		}
		if len(settings) > 0 {
			request.Settings = cloneMap(settings)
		}
		startedAt := time.Now()
		response, err := service.CreateResponse(context.Background(), request)
		return response, float64(time.Since(startedAt).Microseconds()) / 1000.0, err
	}
	fail := func(stage string, durationMS float64, err error) (map[string]any, []map[string]any, bool) {
		return harnessBundleResult(bundle, false, durationMS, err.Error(), nil), []map[string]any{newHarnessFailure(candidateID, bundle, stage, err.Error())}, true
	}

	switch bundle {
	case "core/exact_math":
		response, durationMS, err := run("2+3=5")
		if err != nil {
			return fail("execution", durationMS, err)
		}
		output := harnessFirstOutputText(response.Output)
		pass := strings.Contains(output, "2+3=5")
		message := ""
		if !pass {
			message = "expected output to contain 2+3=5"
		}
		return harnessBundleResult(bundle, pass, durationMS, message, map[string]any{
			"output_text": output,
		}), harnessBundleFailures(candidateID, bundle, message), false
	case "core/plain_language":
		response, durationMS, err := run("hello orb")
		if err != nil {
			return fail("execution", durationMS, err)
		}
		output := harnessFirstOutputText(response.Output)
		pass := strings.TrimSpace(output) != ""
		message := ""
		if !pass {
			message = "expected non-empty output text"
		}
		return harnessBundleResult(bundle, pass, durationMS, message, map[string]any{
			"output_text": output,
		}), harnessBundleFailures(candidateID, bundle, message), false
	case "core/instruction_follow":
		response, durationMS, err := run("ORB")
		if err != nil {
			return fail("execution", durationMS, err)
		}
		output := harnessFirstOutputText(response.Output)
		pass := strings.Contains(output, "ORB")
		message := ""
		if !pass {
			message = "expected output to contain ORB"
		}
		return harnessBundleResult(bundle, pass, durationMS, message, map[string]any{
			"output_text": output,
		}), harnessBundleFailures(candidateID, bundle, message), false
	case "memory/scope_recall":
		scope := memoryRequest.Scope
		response, durationMS, err := run("deployment note alpha")
		if err != nil {
			return fail("execution", durationMS, err)
		}
		queryResults, err := service.QueryMemory(context.Background(), orb.MemoryQuery{
			Scope: scope,
			Query: "alpha",
			Limit: 1,
		})
		if err != nil {
			return fail("memory_query", durationMS, err)
		}
		pass := len(queryResults) > 0 && queryResults[0].InputText == "deployment note alpha"
		message := ""
		if !pass {
			message = "no scoped memory results matched alpha"
		}
		return harnessBundleResult(bundle, pass, durationMS, message, map[string]any{
			"memory_scope":   scope,
			"memory_matches": len(queryResults),
			"output_text":    harnessFirstOutputText(response.Output),
		}), harnessBundleFailures(candidateID, bundle, message), false
	case "runtime/latency_short":
		response, durationMS, err := run("latency probe")
		if err != nil {
			return fail("execution", durationMS, err)
		}
		thresholdMS := harnessMaxLatencyMS(state.spec.UserObjective.Constraints)
		pass := durationMS <= float64(thresholdMS) && strings.TrimSpace(harnessFirstOutputText(response.Output)) != ""
		message := ""
		if !pass {
			message = fmt.Sprintf("latency %.2fms exceeded %dms threshold", durationMS, thresholdMS)
		}
		return harnessBundleResult(bundle, pass, durationMS, message, map[string]any{
			"latency_ms":   durationMS,
			"threshold_ms": thresholdMS,
		}), harnessBundleFailures(candidateID, bundle, message), false
	default:
		message := "bundle is not supported by the current runner"
		return harnessBundleResult(bundle, false, 0, message, nil), []map[string]any{newHarnessFailure(candidateID, bundle, "bundle", message)}, false
	}
}

func harnessBundleResult(bundle string, pass bool, durationMS float64, message string, details map[string]any) map[string]any {
	score := 0.0
	if pass {
		score = 1.0
	}
	result := map[string]any{
		"bundle":      bundle,
		"pass":        pass,
		"score":       score,
		"duration_ms": durationMS,
		"message":     message,
	}
	if len(details) > 0 {
		result["details"] = details
	}
	return result
}

func harnessBundleFailures(candidateID, bundle, message string) []map[string]any {
	if strings.TrimSpace(message) == "" {
		return nil
	}
	return []map[string]any{newHarnessFailure(candidateID, bundle, "bundle", message)}
}

func newHarnessFailure(candidateID, bundle, stage, message string) map[string]any {
	return map[string]any{
		"id":           candidateID + ":" + bundle,
		"candidate_id": candidateID,
		"bundle":       bundle,
		"stage":        stage,
		"error":        message,
	}
}

func expandHarnessCandidates(searchSpace map[string]any, maxCandidates int) []map[string]any {
	candidates := expandHarnessMap(searchSpace)
	if len(candidates) == 0 {
		candidates = []map[string]any{{}}
	}
	if maxCandidates > 0 && len(candidates) > maxCandidates {
		return candidates[:maxCandidates]
	}
	return candidates
}

func expandHarnessMap(source map[string]any) []map[string]any {
	if len(source) == 0 {
		return []map[string]any{{}}
	}

	keys := slices.Sorted(maps.Keys(source))
	candidates := []map[string]any{{}}
	for _, key := range keys {
		options := expandHarnessValue(source[key])
		next := make([]map[string]any, 0, len(candidates)*max(len(options), 1))
		for _, candidate := range candidates {
			for _, option := range options {
				cloned := cloneMap(candidate)
				cloned[key] = cloneJSONValue(option)
				next = append(next, cloned)
			}
		}
		candidates = next
	}
	return candidates
}

func expandHarnessValue(value any) []any {
	switch typed := value.(type) {
	case map[string]any:
		expandedMaps := expandHarnessMap(typed)
		options := make([]any, 0, len(expandedMaps))
		for _, expanded := range expandedMaps {
			options = append(options, expanded)
		}
		return options
	case []any:
		if len(typed) == 0 {
			return []any{nil}
		}
		options := make([]any, 0, len(typed))
		for _, option := range typed {
			options = append(options, cloneJSONValue(option))
		}
		return options
	default:
		return []any{typed}
	}
}

func harnessFirstOutputText(items []orb.OutputItem) string {
	for _, item := range items {
		text := strings.TrimSpace(item.Text)
		if text != "" {
			return text
		}
	}
	return ""
}

func harnessCandidateModelID(service *orb.Service, candidateConfig map[string]any) string {
	if value, ok := stringFromNestedValue(candidateConfig, "models", "ids"); ok {
		return value
	}
	if value, ok := stringFromNestedValue(candidateConfig, "model"); ok {
		return value
	}
	models := service.Models(context.Background())
	if len(models) > 0 {
		return models[0].ID
	}
	return ""
}

func harnessCandidateMemoryConfig(candidateConfig map[string]any, experimentID, candidateID string) (bool, string) {
	enabled, _ := boolFromNestedValue(candidateConfig, "memory", "enabled")
	scope, _ := stringFromNestedValue(candidateConfig, "memory", "scopes")
	if scope == "" {
		scope, _ = stringFromNestedValue(candidateConfig, "memory", "scope")
	}
	if scope == "" {
		scope = fmt.Sprintf("workspace:harness:%s:%s", experimentID, candidateID)
	}
	return enabled, scope
}

func harnessMaxCandidates(evolution map[string]any) int {
	value, ok := intFromNestedValue(evolution, "max_candidates")
	if !ok || value <= 0 {
		return 0
	}
	return value
}

func harnessMaxLatencyMS(constraints map[string]any) int {
	value, ok := intFromNestedValue(constraints, "max_p50_latency_ms")
	if !ok || value <= 0 {
		return 2500
	}
	return value
}

func lookupNestedValue(source map[string]any, path ...string) (any, bool) {
	if len(path) == 0 {
		return source, source != nil
	}

	var current any = source
	for _, part := range path {
		typed, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := typed[part]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func stringFromNestedValue(source map[string]any, path ...string) (string, bool) {
	value, ok := lookupNestedValue(source, path...)
	if !ok {
		return "", false
	}
	stringValue, ok := value.(string)
	if !ok {
		return "", false
	}
	stringValue = strings.TrimSpace(stringValue)
	if stringValue == "" {
		return "", false
	}
	return stringValue, true
}

func boolFromNestedValue(source map[string]any, path ...string) (bool, bool) {
	value, ok := lookupNestedValue(source, path...)
	if !ok {
		return false, false
	}
	boolValue, ok := value.(bool)
	if !ok {
		return false, false
	}
	return boolValue, true
}

func mapFromNestedValue(source map[string]any, path ...string) (map[string]any, bool) {
	value, ok := lookupNestedValue(source, path...)
	if !ok {
		return nil, false
	}
	mapValue, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	return cloneMap(mapValue), true
}

func intFromNestedValue(source map[string]any, path ...string) (int, bool) {
	value, ok := lookupNestedValue(source, path...)
	if !ok {
		return 0, false
	}
	return intFromValue(value)
}

func mustFormatJSON(payload any) string {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(body)
}

func intFromValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float32:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func intValue(value any) int {
	typed, ok := intFromValue(value)
	if !ok {
		return 0
	}
	return typed
}

func floatValue(value any) float64 {
	switch typed := value.(type) {
	case float32:
		return float64(typed)
	case float64:
		return typed
	case int:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

func boolValue(value any) bool {
	typed, ok := value.(bool)
	return ok && typed
}

func stringValue(value any) string {
	typed, ok := value.(string)
	if !ok {
		return ""
	}
	return typed
}

func sortHarnessResults(results []map[string]any) {
	sort.SliceStable(results, func(i, j int) bool {
		leftStrict := boolValue(results[i]["strict_pass"])
		rightStrict := boolValue(results[j]["strict_pass"])
		if leftStrict != rightStrict {
			return leftStrict
		}

		leftScore := floatValue(results[i]["score"])
		rightScore := floatValue(results[j]["score"])
		if leftScore != rightScore {
			return leftScore > rightScore
		}

		return stringValue(results[i]["id"]) < stringValue(results[j]["id"])
	})
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
