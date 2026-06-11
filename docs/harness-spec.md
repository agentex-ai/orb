# Harness Experiment Spec Draft

This document outlines the first planned experiment-spec shape for Agentex Orb
Harness.

The goal is to give Orb a repeatable way to compare runtime candidates across
model choice, routing policy, memory configuration, execution settings, and
private or hosted deployment paths.

This is a planning draft, not an implemented contract yet.

## Design Goals

- Reuse the same Orb-compatible execution API that applications call.
- Keep the harness control plane separate from the inference surface.
- Make search spaces explicit instead of hiding candidate generation in code.
- Support baseline-aware promotion decisions.
- Produce durable artifacts that can be reviewed or replayed later.

## Top-Level Shape

The planned top-level spec should keep the same broad rhythm as a practical
experiment runner:

```json
{
  "experiment_id": "private-routing-memory-sweep-20260611",
  "user_objective": {
    "primary": "balanced",
    "constraints": {
      "min_quality_score": 0.85,
      "strict_pass_required": true,
      "quality_fallback": "baseline_usable",
      "usable_quality_score": 0.7,
      "quality_regression_policy": "baseline_floor",
      "max_p50_latency_ms": 2500,
      "max_error_rate": 0.02
    }
  },
  "bundles": [
    "core/exact_math",
    "core/plain_language",
    "memory/scope_recall",
    "runtime/latency_short"
  ],
  "search_space": {
    "models": {
      "ids": ["orb/private/qwen3-32b", "orb/openai/gpt-5-mini"]
    },
    "routing": {
      "policies": ["direct", "provider_fallback"]
    },
    "memory": {
      "enabled": [false, true],
      "scopes": ["workspace:support"],
      "top_k": [3, 5]
    },
    "execution_settings": {
      "max_output_tokens": [128, 256],
      "temperature": [0.0, 0.2]
    },
    "harness": {
      "prompt_sets": ["support_short"],
      "concurrency": [1, 2],
      "rounds": [3],
      "warmup_runs": [1]
    }
  },
  "execution": {
    "base_url": "http://127.0.0.1:18080",
    "preferred_language": "en",
    "request_timeout_ms": 45000,
    "benchmark": {
      "max_tokens": 256,
      "concurrency": 1,
      "warmup": 1,
      "runs": 5
    },
    "managed_server": {
      "command": "go",
      "args": ["run", "./cmd/orb"],
      "workdir": ".",
      "env": {
        "ORB_ADDR": "127.0.0.1:18080",
        "ORB_PRIVATE_BASE_URL": "http://127.0.0.1:19080"
      },
      "ready_url": "http://127.0.0.1:18080/v1/models",
      "startup_timeout_ms": 30000
    }
  },
  "evolution": {
    "strategy": "grid",
    "max_candidates": 48,
    "early_stop_failures": 6,
    "random_seed": 42
  }
}
```

A matching example file lives at:

- [examples/harness/private-routing-memory-sweep.json](../examples/harness/private-routing-memory-sweep.json)

## Sections

- `experiment_id`: unique identifier for tracking and artifacts.
- `user_objective`: optimization target plus hard constraints.
- `bundles`: named evaluation bundles to run for each candidate.
- `search_space`: runtime-facing dimensions the harness may sweep.
- `execution`: how the harness reaches a running Orb-compatible runtime.
- `evolution`: how candidate generation or traversal should behave.

## User Objective

Suggested primary objective values:

- `latency`
- `throughput`
- `quality`
- `resource_efficiency`
- `balanced`

Suggested constraint fields:

- `min_quality_score`
- `strict_pass_required`
- `quality_fallback`
- `usable_quality_score`
- `quality_regression_policy`
- `max_p50_latency_ms`
- `max_error_rate`
- `max_cost_per_1k_output_tokens`

The harness should keep a clear separation between hard gates, soft ranking, and
fallback visibility.

## Search Space

Orb Harness should focus first on runtime-facing dimensions rather than
lower-level quantization or device-tuning knobs.

Suggested sections:

- `models`: Orb-visible model ids.
- `routing`: direct routing, fallback order, and future policy strategies.
- `memory`: enabled state, scope shape, retrieval depth, and future memory
  backends.
- `execution_settings`: temperature, output-token limits, and future exposed
  adapter settings.
- `harness`: prompt sets, concurrency, rounds, and warmup behavior.

## Execution

The `execution` object should describe how the harness talks to a runtime.

Suggested fields:

- `base_url`
- `api_key_env`
- `preferred_language`
- `request_timeout_ms`
- `benchmark`
- `managed_server`

The `managed_server` block should make it possible to launch a dedicated Orb
runtime for evaluation, wait for readiness, and shut it down afterward.

## Evolution

Suggested evolution fields:

- `strategy`: `grid`, `random`, `neighborhood`, or future adaptive strategies.
- `max_candidates`
- `early_stop_failures`
- `random_seed`

## Baseline and Artifacts

Promotion decisions should be able to compare a candidate against a previously
saved baseline.

The harness should eventually write a stable artifact set for each experiment,
such as:

- `plan.json`
- `summary.json`
- `promotion.json`
- `pareto_front.json`
- `failures.json`
- `report.md`

## Status

Planned, not implemented.