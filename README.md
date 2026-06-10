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

## Quick Start

Run the current Orb skeleton locally:

```bash
go run ./cmd/orb
```

The server listens on `:8080` by default. Set `ORB_ADDR` to override the bind
address.

Optional private routing environment variables:

- `ORB_PRIVATE_BASE_URL`: upstream Orb-compatible private runtime base URL
- `ORB_PRIVATE_MODEL_ID`: optional local model id override for single-model private routing; defaults to `orb/private-example-text` when single-model mode is enabled
- `ORB_PRIVATE_UPSTREAM_MODEL`: optional upstream model id override for single-model private routing
- `ORB_PRIVATE_AUTH_HEADER`: auth header name for upstream private requests, defaults to `Authorization`
- `ORB_PRIVATE_AUTH_TOKEN`: auth token for upstream private requests; when using `Authorization`, Orb sends `Bearer <token>` unless the token already contains a space

Current placeholder endpoints:

- `GET /v1/models`
- `POST /v1/responses`

The current runtime uses a model-routed adapter registry. The default registry
currently exposes:

- a bundled local `echo` adapter with the model `orb/example-text`
- a bundled private-style `echo` adapter with the model `orb/private-example-text`

When `ORB_PRIVATE_BASE_URL` is configured, the bundled private route is replaced
by a `private-http` adapter that forwards `POST /v1/responses` calls to the
upstream runtime. Optional auth headers can be attached through
`ORB_PRIVATE_AUTH_HEADER` and `ORB_PRIVATE_AUTH_TOKEN`.

Private routing currently supports two modes:

- automatic discovery mode: if only `ORB_PRIVATE_BASE_URL` is set, Orb queries
  the upstream `GET /v1/models` endpoint and exposes each discovered model as
  `orb/private/<upstream-id>`
- single-model override mode: if `ORB_PRIVATE_MODEL_ID` or
  `ORB_PRIVATE_UPSTREAM_MODEL` is set, Orb exposes one forwarded private model
  and keeps the earlier explicit mapping behavior

When private routing is configured, `GET /v1/models` includes discovery
metadata for forwarded private models, including the upstream model id and
provider when available.

## Architecture Direction

Orb is expected to evolve around a small set of runtime responsibilities:

- Accept model execution requests through a public API surface.
- Normalize model and provider differences through adapters.
- Route requests to hosted, private, or local model backends.
- Add optional memory and context layers before execution.
- Apply post-processing, policy, usage tracking, and observability after
  execution.

Concrete API schemas, deployment topology, memory backends, and longer-term
runtime contracts will keep evolving as the current skeleton grows into a real
runtime.

## Status

Early implementation skeleton.

This repository currently exists to establish the public home for Agentex Orb
and to document its intended direction. It now includes a minimal HTTP service,
an adapter-backed runtime skeleton, bundled local/private echo adapters, and an
upstream private HTTP adapter with model discovery. It does not yet contain
production runtime code.

## License

MIT
