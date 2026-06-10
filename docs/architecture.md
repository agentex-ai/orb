# Architecture Direction

Agentex Orb is intended to be the runtime core behind Agentex model APIs and
runtime services.

This document outlines the architectural direction for Orb before
implementation-specific decisions are locked in.

## Goals

- Provide a unified API surface for model execution.
- Support hosted, private, and self-managed model backends.
- Add optional memory and context augmentation around model calls.
- Normalize provider differences through adapter interfaces.
- Leave room for policy enforcement, telemetry, and usage accounting.

## System Boundary

Orb is planned as the layer between application-facing APIs and model/provider
backends.

Applications and services should be able to call Orb without needing to know:

- which model provider handled the request,
- whether memory augmentation was applied,
- whether the target model was hosted, private, or local,
- which post-processing or policy hooks ran after execution.

## Planned Runtime Components

### API Surface

The public API layer should accept model execution requests and expose stable
runtime operations such as model listing, execution, and runtime status.

### Routing Layer

The routing layer should choose the target backend or model path based on
request shape, policy, deployment mode, and future runtime strategy.

### Adapter Layer

Adapters should translate between Orb's internal request and response model and
provider-specific APIs or private model runtimes.

### Memory Layer

The memory layer should optionally enrich a request with prior context, cached
state, retrieved memories, or application-scoped conversation history.

### Policy and Post-Processing

This layer should support guardrails, response shaping, redaction, validation,
usage accounting, and future workflow hooks.

### Telemetry and Observability

Orb should record runtime metadata needed for debugging, performance
measurement, and product-level usage visibility.

## Request Lifecycle

The expected execution path is:

1. Accept request through the Orb API.
2. Validate and normalize the request into Orb's internal runtime shape.
3. Enrich the request with optional memory and context.
4. Select a target model path through routing rules.
5. Execute the request through the selected adapter.
6. Apply post-processing, policy, and usage tracking.
7. Return the final response plus relevant runtime metadata.

## Early Non-Goals

The first implementation should avoid overcommitting to:

- a fixed provider set,
- a finalized wire protocol beyond a minimum stable API,
- a single memory backend strategy,
- deployment assumptions tied to one cloud or one runtime language.

## Current Status

Early implementation skeleton.

The repository now includes a minimal Go HTTP service, a model-routed adapter
registry, bundled local and private echo adapters, a hosted OpenAI adapter, and
an upstream private HTTP adapter with model discovery.

Implementation details such as storage backends, memory architecture,
deployment model, and broader runtime contracts remain open and should keep
evolving in follow-up design work.
