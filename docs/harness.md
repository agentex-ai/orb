# Harness Direction

Agentex Orb is expected to grow a harness layer alongside its inference and
runtime surface.

## What Orb Harness Is

Orb Harness is the planned repeatable experiment and evaluation layer for
Agentex Orb. It is intended to compare runtime candidates, model routes,
memory strategies, and policy configurations against consistent bundles of
checks.

## Planned Responsibilities

The long-term harness direction should support:

- reusable evaluation bundles for strict quality, instruction following, tool
  use, latency, throughput, and resource-efficiency checks,
- experiment specs that sweep model, adapter, memory, routing, and policy
  search spaces,
- baseline comparison and promotion decisions,
- managed-server execution for private or local Orb-compatible runtimes,
- durable artifacts such as plans, summaries, failure logs, reports, and
  recommendation outputs.

## Relationship to the Orb API

The harness layer should reuse the same Orb-compatible execution endpoints
where possible.

It is not the same thing as `/v1/responses` or `/v1/runs`; it sits beside the
runtime surface as an evaluation and control plane.

## Early Shape

Early harness work will likely center on:

- bundle definitions,
- experiment specs,
- runner and orchestration interfaces,
- scoring and promotion policy,
- report and artifact generation.

## Related Drafts

- [Harness Experiment Spec Draft](harness-spec.md)
- [Harness Control Plane Draft](harness-api.md)
- [Example Private Routing Sweep Spec](../examples/harness/private-routing-memory-sweep.json)

## Status

Planned, not implemented.