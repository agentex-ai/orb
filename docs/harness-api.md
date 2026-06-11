# Harness Control Plane Draft

This document outlines the first planned control-plane API shape for Agentex Orb
Harness.

The inference API should remain focused on `/v1/models`, `/v1/responses`,
`/v1/memory/query`, and `/v1/runs`.

Harness orchestration should sit beside that execution plane under a separate
control path.

## Base Path

The planned harness control plane should live under:

`/api/v1/harness`

## Design Principles

- Keep experiment orchestration separate from normal inference calls.
- Reuse Orb-compatible execution endpoints underneath the harness.
- Make status, artifacts, and promotion outputs fetchable through stable ids.
- Prefer explicit experiment resources over hidden background jobs.
- Return enough detail for a UI, CLI, or CI workflow to operate against the
  same control plane.

## Planned Endpoints

- `GET /api/v1/harness/bundles`
- `POST /api/v1/harness/experiments`
- `GET /api/v1/harness/experiments`
- `GET /api/v1/harness/experiments/{experiment_id}`
- `GET /api/v1/harness/experiments/{experiment_id}/artifacts/{artifact}`
- `POST /api/v1/harness/candidates/{candidate_id}/apply`

## Bundles

`GET /api/v1/harness/bundles` should return the bundle catalog visible to the
current harness runtime.

Example response:

```json
{
  "object": "list",
  "data": [
    {
      "id": "core/exact_math",
      "name": "Exact Math",
      "category": "core",
      "description": "Exact arithmetic gates such as 2+3=5 in zh/en.",
      "default_enabled": true
    },
    {
      "id": "memory/scope_recall",
      "name": "Memory Scope Recall",
      "category": "runtime",
      "description": "Checks scoped memory retrieval and reuse behavior."
    }
  ]
}
```

## Experiments

`POST /api/v1/harness/experiments` should start a new harness experiment from a
spec document.

Example accepted response:

```json
{
  "experiment_id": "private-routing-memory-sweep-20260611",
  "object": "harness.experiment",
  "state": "queued",
  "status": "Experiment accepted",
  "progress": 0,
  "created_at": "2026-06-11T14:00:00Z",
  "artifacts": {
    "status_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611"
  }
}
```

`GET /api/v1/harness/experiments` should list known experiments.

Example response:

```json
{
  "object": "list",
  "data": [
    {
      "experiment_id": "private-routing-memory-sweep-20260611",
      "state": "running",
      "status": "Evaluating candidate 8 of 24",
      "progress": 33,
      "created_at": "2026-06-11T14:00:00Z",
      "updated_at": "2026-06-11T14:07:12Z",
      "objective": "balanced"
    }
  ]
}
```

`GET /api/v1/harness/experiments/{experiment_id}` should return the current
status snapshot plus results when available.

Example response shape:

```json
{
  "experiment_id": "private-routing-memory-sweep-20260611",
  "state": "completed",
  "status": "Experiment completed",
  "progress": 100,
  "created_at": "2026-06-11T14:00:00Z",
  "updated_at": "2026-06-11T14:15:41Z",
  "objective": "balanced",
  "artifacts": {
    "plan_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/plan",
    "summary_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/summary",
    "promotion_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/promotion",
    "report_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/report"
  },
  "summary": {
    "total_candidates": 24,
    "successful_candidates": 18,
    "failed_candidates": 6,
    "strict_promoted": 2,
    "rejected": 16,
    "duration_seconds": 941
  },
  "results": [
    {
      "id": "cand_0008",
      "score": 0.91,
      "quality_score": 0.89,
      "decode_tps": 82.4,
      "ttft_ms": 1180,
      "strict_pass": true,
      "promotion": "strict",
      "config": {
        "model": "orb/private/qwen3-32b",
        "routing_policy": "provider_fallback",
        "memory": {
          "enabled": true,
          "scope": "workspace:support",
          "top_k": 5
        }
      }
    }
  ],
  "failures": [
    {
      "id": "cand_0004",
      "stage": "execution",
      "error": "upstream private runtime timed out"
    }
  ]
}
```

## Artifacts

`GET /api/v1/harness/experiments/{experiment_id}/artifacts/{artifact}` should
return a materialized experiment artifact.

Suggested artifact keys:

- `plan`
- `summary`
- `promotion`
- `pareto_front`
- `failures`
- `report`

The first five should normally be JSON. `report` should normally be Markdown.

## Candidate Apply

`POST /api/v1/harness/candidates/{candidate_id}/apply` should apply a selected
candidate configuration to a target Orb runtime or save it as the active
recommended config.

Example request:

```json
{
  "experiment_id": "private-routing-memory-sweep-20260611",
  "target": "runtime",
  "dry_run": false
}
```

Example response:

```json
{
  "candidate_id": "cand_0008",
  "experiment_id": "private-routing-memory-sweep-20260611",
  "object": "harness.apply",
  "status": "applied",
  "applied_at": "2026-06-11T14:17:03Z"
}
```

## Error Shape

The harness control plane should use the same shared JSON error envelope style
as the main Orb API:

```json
{
  "error": {
    "code": "invalid_argument",
    "message": "experiment_id is required"
  }
}
```

Additional likely error codes for the control plane include:

- `already_exists`
- `experiment_not_found`
- `candidate_not_found`
- `artifact_not_found`
- `experiment_conflict`
- `experiment_failed`

## Relationship to the Runtime API

The harness API should not replace `/v1/responses` or `/v1/runs`.

Instead, it should submit Orb-compatible runtime calls under the hood, collect
metrics and bundle results, write artifacts, and expose promotion or apply
workflows.

## Status

Planned, not implemented.