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
}

type harnessExperimentState struct {
	spec      HarnessExperimentRequest
	state     string
	status    string
	progress  int
	createdAt time.Time
	updatedAt time.Time
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
		result = append(result, state.summary())
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
	return &cloned
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}

	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func (s *harnessExperimentState) summary() HarnessExperimentSummary {
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

	state, ok := s.harness.get(experimentID)
	if !ok {
		writeError(writer, http.StatusNotFound, APIError{
			Code:    "experiment_not_found",
			Message: fmt.Sprintf("experiment %q was not found", experimentID),
			Details: map[string]any{"experiment_id": experimentID},
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

	writeError(writer, http.StatusNotFound, APIError{
		Code:    "artifact_not_found",
		Message: fmt.Sprintf("artifact %q is not yet materialized for experiment %q", artifact, experimentID),
		Details: map[string]any{"experiment_id": experimentID, "artifact": artifact, "state": state.state},
	})
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
