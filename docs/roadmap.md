# Roadmap

This roadmap describes the intended path from public planning repository to a
first usable Orb runtime.

## Phase 0: Public Direction

- Establish the public repository and product naming.
- Publish core positioning, architecture direction, and roadmap.
- Keep implementation commitments intentionally light while scope is still being
  shaped.

## Phase 1: Runtime Shape

- Define the first public API surface for model execution.
- Choose the initial implementation language and service layout.
- Define the internal request and response model used by adapters.
- Decide the first provider and private-model adapter targets.

## Phase 2: Minimum Runtime

- Implement a basic execution API.
- Add one hosted adapter path and one private-model path.
- Add request normalization, routing, and structured error handling.
- Add logging, telemetry hooks, and basic operational visibility.

## Phase 3: Memory and Policy

- Add optional memory-backed request enrichment.
- Add policy and post-processing hooks around execution.
- Define boundaries between runtime memory, application context, and provider
  prompts.
- Add runtime-level usage accounting and request tracing.

## Phase 4: Hardening

- Improve deployment guidance for private environments.
- Expand adapter coverage and routing strategy.
- Add conformance tests for adapters and runtime behavior.
- Stabilize public API contracts and operational documentation.

## Current Status

Orb is currently between Phase 1 and early Phase 2.

The repository now has a minimal HTTP runtime skeleton, bundled local/private
adapters, a hosted OpenAI adapter, and a private HTTP forwarding path with
upstream model discovery.

The next meaningful milestone is to keep tightening the runtime boundary,
expanding provider coverage, and adding memory hooks plus operational
hardening.
