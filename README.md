# Agentex Orb

Agentex Orb is the model runtime core for Agentex.

Orb is planned as the unified runtime layer for AI APIs, private model deployment,
memory-augmented execution, model routing, extensible model adapters, and
repeatable runtime evaluation harnesses. It is intended to sit behind Agentex
products and APIs as the place where model calls, runtime policy, context,
memory, adapter behavior, and runtime evaluation come together.

## What Orb Is

Orb is the planned model execution layer for Agentex. It is not only an
inference endpoint; it is intended to provide the runtime surface around
inference as well.

The long-term goal is to support:

- Unified API access to model inference.
- Private and self-hosted model deployment.
- Built-in memory and context augmentation.
- Model routing and adapter-based provider integration.
- Repeatable evaluation harnesses for runtime, model, memory, and policy
  configurations.
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
- **Orb Harness**: repeatable experiment and evaluation harnesses for runtime
  candidates, memory configurations, routing policies, and promotion reporting.
- **Orb Adapters**: provider and model integration points for first-party,
  third-party, and private models.

## Documents

- [Architecture Direction](docs/architecture.md)
- [API Draft](docs/api.md)
- [OpenAPI Spec](docs/openapi.yaml)
- [Roadmap](docs/roadmap.md)
- [Harness Direction](docs/harness.md)
- [Harness Experiment Spec Draft](docs/harness-spec.md)
- [Harness Control Plane Draft](docs/harness-api.md)
- [TypeScript SDK Skeleton](sdk/typescript/README.md)
- [Python SDK Skeleton](sdk/python/README.md)

## Quick Start

Run the current Orb skeleton locally:

```bash
go run ./cmd/orb
```

The server listens on `:8080` by default. Set `ORB_ADDR` to override the bind
address.

Optional OpenAI hosted routing environment variables:

- `ORB_OPENAI_API_KEY`: OpenAI API key for the hosted adapter
- `ORB_OPENAI_MODEL_ID`: upstream OpenAI model id to call, such as `gpt-5-mini`
- `ORB_OPENAI_PUBLIC_MODEL_ID`: optional Orb-visible model id override; defaults to `orb/openai/<model-id>`
- `ORB_OPENAI_BASE_URL`: optional OpenAI-compatible base URL override; defaults to `https://api.openai.com/v1`

Optional private routing environment variables:

- `ORB_PRIVATE_BASE_URL`: upstream Orb-compatible private runtime base URL
- `ORB_PRIVATE_MODEL_ID`: optional local model id override for single-model private routing; defaults to `orb/private-example-text` when single-model mode is enabled
- `ORB_PRIVATE_UPSTREAM_MODEL`: optional upstream model id override for single-model private routing
- `ORB_PRIVATE_AUTH_HEADER`: auth header name for upstream private requests, defaults to `Authorization`
- `ORB_PRIVATE_AUTH_TOKEN`: auth token for upstream private requests; when using `Authorization`, Orb sends `Bearer <token>` unless the token already contains a space

Current implemented endpoints:

- `GET /v1/models`
- `POST /v1/responses`
- `GET /v1/responses/{response_id}`
- `POST /v1/memory/query`
- `POST /v1/runs`
- `GET /api/v1/harness/bundles`
- `POST /api/v1/harness/experiments`
- `GET /api/v1/harness/experiments`
- `GET /api/v1/harness/experiments/{experiment_id}`
- `GET /api/v1/harness/experiments/{experiment_id}/artifacts/{artifact}`
Try the bundled local model:

```bash
curl http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "orb/example-text",
    "input": [
      {
        "role": "user",
        "content": [
          {
            "type": "input_text",
            "text": "hello orb"
          }
        ]
      }
    ]
  }'
```

Current response retrieval stores completed non-stream responses in memory for
the life of the current server process. `GET /v1/responses/{response_id}` can
read those responses back until the process restarts. Streamed responses are not
stored yet.

The current memory query path is also in-memory. When a non-stream request is
sent with `"memory":{"enabled":true,"scope":"..."}`, Orb stores the request
input and response output as a lightweight memory entry for that scope.

Example memory query after creating one or more memory-enabled responses:

```bash
curl http://localhost:8080/v1/memory/query \
  -H "Content-Type: application/json" \
  -d '{
    "scope": "workspace:test",
    "query": "hello",
    "limit": 5
  }'
```

The current `POST /v1/runs` path is a thin wrapper around the same execution
flow used by `POST /v1/responses`. It currently accepts the same request body
shape and returns the same JSON or SSE response shapes.

The current runtime uses a model-routed adapter registry. The default registry
currently exposes:

- a bundled local `echo` adapter with the model `orb/example-text`
- a bundled private-style `echo` adapter with the model `orb/private-example-text`

When `ORB_OPENAI_API_KEY` and `ORB_OPENAI_MODEL_ID` are configured, Orb also
exposes a hosted OpenAI-backed model. By default, that model is visible as
`orb/openai/<model-id>` and is executed through OpenAI's Responses API.

That hosted path also supports streaming through `POST /v1/responses` with a
top-level `"stream": true` field. Orb returns server-sent events and currently
passes through OpenAI's typed event names such as `response.created`,
`response.output_text.delta`, `response.completed`, and `error`.

Example streamed hosted request after setting `ORB_OPENAI_API_KEY` and
`ORB_OPENAI_MODEL_ID=gpt-5-mini`:

```bash
curl -N http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "orb/openai/gpt-5-mini",
    "stream": true,
    "input": [
      {
        "role": "user",
        "content": [
          {
            "type": "input_text",
            "text": "Write one short line about Agentex Orb."
          }
        ]
      }
    ]
  }'
```

When `ORB_PRIVATE_BASE_URL` is configured, the bundled private route is replaced
by a `private-http` adapter that forwards `POST /v1/responses` calls to the
upstream runtime. Optional auth headers can be attached through
`ORB_PRIVATE_AUTH_HEADER` and `ORB_PRIVATE_AUTH_TOKEN`.

If the upstream private runtime supports streaming, Orb also forwards
`"stream": true` requests for private models and returns server-sent events from
the upstream runtime.

Example streamed private request after setting `ORB_PRIVATE_BASE_URL`:

```bash
curl -N http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "orb/private/qwen3-32b",
    "stream": true,
    "input": [
      {
        "role": "user",
        "content": [
          {
            "type": "input_text",
            "text": "Give me one short deployment note."
          }
        ]
      }
    ]
  }'
```

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
- Run repeatable harness experiments against model, routing, memory, and policy
  candidates.
- Apply post-processing, policy, usage tracking, and observability after
  execution.

Concrete API schemas, deployment topology, memory backends, and longer-term
runtime contracts will keep evolving as the current skeleton grows into a real
runtime.

## Status

Early implementation skeleton.

This repository currently exists to establish the public home for Agentex Orb
and to document its intended direction. It now includes a minimal HTTP service,
an adapter-backed runtime skeleton, bundled local/private echo adapters, a real
hosted OpenAI adapter with streaming support, an upstream private HTTP adapter
with model discovery and streaming pass-through, and an early in-memory harness
runner that can expand candidate search spaces and execute a small built-in
bundle set against the live Orb runtime surface. It does not yet contain
production runtime code or a full harness execution, persistence, and
promotion plane.
## License

MIT
