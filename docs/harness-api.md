# Harness Control Plane Draft

This document outlines the current control-plane shape for Agentex Orb
Harness, including the early in-memory runner already implemented in this
repository.

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

## Endpoints

- `GET /api/v1/harness/bundles`
- `POST /api/v1/harness/experiments`
- `GET /api/v1/harness/experiments`
- `GET /api/v1/harness/experiments/{experiment_id}`
- `GET /api/v1/harness/experiments/{experiment_id}/artifacts/{artifact}`
- `POST /api/v1/harness/candidates/{candidate_id}/apply`

The first five are implemented today as an in-memory control plane and runner.
`POST /api/v1/harness/candidates/{candidate_id}/apply` remains planned.

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
      "category": "memory",
      "description": "Checks scoped memory retrieval and reuse behavior."
    }
  ]
}
```

## Experiments

`POST /api/v1/harness/experiments` starts a new harness experiment from a spec
document.

In the current runner implementation, Orb returns `202 Accepted` with an
initial queued snapshot, then expands candidates and executes a small built-in
bundle set immediately in the same process. A following `GET` will usually
observe the experiment in the completed state.

Example accepted response:

```json
{
  "experiment_id": "private-routing-memory-sweep-20260611",
  "object": "harness.experiment",
  "state": "queued",
  "status": "Experiment accepted",
  "progress": 0,
  "created_at": "2026-06-11T14:00:00Z",
  "updated_at": "2026-06-11T14:00:00Z",
  "objective": "balanced",
  "bundles": [
    "core/exact_math",
    "memory/scope_recall"
  ],
  "artifacts": {
    "status_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611",
    "plan_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/plan",
    "summary_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/summary",
    "promotion_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/promotion",
    "pareto_front_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/pareto_front",
    "failures_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/failures",
    "report_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/report"
  }
}
```

`GET /api/v1/harness/experiments` lists known experiments.

Example response:

```json
{
  "object": "list",
  "data": [
    {
      "experiment_id": "private-routing-memory-sweep-20260611",
      "state": "completed",
      "status": "Experiment completed",
      "progress": 100,
      "created_at": "2026-06-11T14:00:00Z",
      "updated_at": "2026-06-11T14:00:01Z",
      "objective": "balanced"
    }
  ]
}
```

`GET /api/v1/harness/experiments/{experiment_id}` returns the current status
snapshot plus results when available.

Example response shape:

```json
{
  "experiment_id": "private-routing-memory-sweep-20260611",
  "object": "harness.experiment",
  "state": "completed",
  "status": "Experiment completed",
  "progress": 100,
  "created_at": "2026-06-11T14:00:00Z",
  "updated_at": "2026-06-11T14:00:01Z",
  "objective": "balanced",
  "bundles": [
    "core/exact_math",
    "memory/scope_recall"
  ],
  "artifacts": {
    "plan_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/plan",
    "summary_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/summary",
    "promotion_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/promotion",
    "pareto_front_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/pareto_front",
    "failures_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/failures",
    "report_path": "/api/v1/harness/experiments/private-routing-memory-sweep-20260611/artifacts/report"
  },
  "summary": {
    "object": "harness.summary",
    "mode": "runner",
    "status": "completed",
    "total_candidates": 4,
    "successful_candidates": 4,
    "failed_candidates": 0,
    "strict_promoted": 2,
    "rejected": 2,
    "duration_seconds": 0
  },
  "results": [
    {
      "id": "cand_0003",
      "model": "orb/example-text",
      "score": 1,
      "quality_score": 1,
      "strict_pass": true,
      "promotion": "strict",
      "memory_enabled": true,
      "memory_scope": "workspace:support",
      "bundle_passes": 2,
      "bundle_total": 2,
      "execution_failed": false,
      "config": {
        "models": {
          "ids": "orb/example-text"
        },
        "memory": {
          "enabled": true,
          "scope": "workspace:support"
        }
      },
      "bundle_results": [
        {
          "bundle": "core/exact_math",
          "pass": true,
          "score": 1
        },
        {
          "bundle": "memory/scope_recall",
          "pass": true,
          "score": 1
        }
      ]
    },
    {
      "id": "cand_0001",
      "model": "orb/example-text",
      "score": 0.5,
      "quality_score": 0.5,
      "strict_pass": false,
      "promotion": "rejected",
      "memory_enabled": false,
      "memory_scope": "workspace:support",
      "bundle_passes": 1,
      "bundle_total": 2,
      "execution_failed": false,
      "config": {
        "models": {
          "ids": "orb/example-text"
        },
        "memory": {
          "enabled": false,
          "scope": "workspace:support"
        }
      }
    }
  ],
  "failures": [
    {
      "id": "cand_0001:memory/scope_recall",
      "candidate_id": "cand_0001",
      "bundle": "memory/scope_recall",
      "stage": "bundle",
      "error": "no scoped memory results matched alpha"
    }
  ]
}
```

## Artifacts

`GET /api/v1/harness/experiments/{experiment_id}/artifacts/{artifact}` returns
a materialized experiment artifact.

Suggested artifact keys:

- `plan`
- `summary`
- `promotion`
- `pareto_front`
- `failures`
- `report`

In the current runner implementation, the first five return JSON and `report`
returns Markdown.

The materialized payloads are currently early runner outputs:

- `plan`: expanded candidate plan and search-space snapshot
- `summary`: aggregate candidate counts and strict-promotion totals
- `promotion`: top-ranked candidate description
- `pareto_front`: top-ranked candidate list
- `failures`: recorded bundle or execution failures
- `report`: Markdown report describing the evaluated run

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

Partially implemented.

Bundle discovery, experiment registration, experiment fetch/list, candidate
expansion, and a small built-in runtime-backed bundle set are implemented
in-memory. Candidate apply, persistence, asynchronous orchestration, and
broader evaluation coverage are still planned.
