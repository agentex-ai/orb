# Agentex Orb

Agentex Orb is the model runtime core for Agentex.

Orb is planned as the unified runtime layer for AI APIs, private model deployment,
memory-augmented execution, model routing, and extensible model adapters. It is
intended to sit behind Agentex products and APIs as the place where model calls,
runtime policy, context, memory, and adapter behavior come together.

## What Orb Is

Orb is the planned model execution layer for Agentex. It is not only an
inference endpoint; it is intended to provide the runtime surface around
inference as well.

The long-term goal is to support:

- Unified API access to model inference.
- Private and self-hosted model deployment.
- Built-in memory and context augmentation.
- Model routing and adapter-based provider integration.
- Runtime hooks for post-processing, policy, telemetry, and future Agentex
  capabilities.

## Planned Capabilities

- **Orb API**: a stable API surface for model execution and runtime operations.
- **Orb Runtime**: the execution core for routing, adapter selection, context
  handling, and response orchestration.
- **Orb Private**: deployment paths for private models and controlled
  environments.
- **Orb Memory**: optional memory-backed execution for applications that need
  persistent context.
- **Orb Adapters**: provider and model integration points for first-party,
  third-party, and private models.

## Documents

- [Architecture Direction](docs/architecture.md)
- [API Draft](docs/api.md)
- [Roadmap](docs/roadmap.md)

## Architecture Direction

Orb is expected to evolve around a small set of runtime responsibilities:

- Accept model execution requests through a public API surface.
- Normalize model and provider differences through adapters.
- Route requests to hosted, private, or local model backends.
- Add optional memory and context layers before execution.
- Apply post-processing, policy, usage tracking, and observability after
  execution.

Concrete API schemas, implementation language, deployment topology, and runtime
contracts will be defined separately as the project moves from planning into
implementation.

## Status

Early planning / pre-implementation.

This repository currently exists to establish the public home for Agentex Orb
and to document its intended direction. It does not yet contain production
runtime code.

## License

MIT
