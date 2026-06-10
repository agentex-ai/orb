# API Draft

This document outlines the first public API shape for Agentex Orb.

The goal of this draft is to define a small, stable surface that can support:

- model discovery,
- request execution,
- optional memory-backed execution,
- future runtime metadata and policy hooks.

This is still an early draft. Field names and wire contracts may evolve as the
runtime skeleton grows into a fuller implementation.

## Design Principles

- Keep the first API small and execution-focused.
- Prefer one primary execution endpoint over many task-specific endpoints.
- Support hosted, private, and local backends behind one runtime contract.
- Make memory augmentation optional rather than implicit.
- Return enough metadata for debugging and runtime visibility.

## Base Path

The expected first version should live under:

`/v1`

The initial API surface should include:

- `GET /v1/models`
- `POST /v1/responses`
- `GET /v1/responses/{response_id}`
- `POST /v1/memory/query`
- `POST /v1/runs`

## Resource Overview

### Models

`GET /v1/models` returns the models currently visible through the Orb runtime.

Each model record should be able to describe:

- stable model identifier,
- provider or adapter source,
- deployment type such as hosted or private,
- capability hints such as text generation, multimodal input, or tool use,
- availability status when relevant.

Example response:

```json
{
  "data": [
    {
      "id": "orb/gpt-4.1",
      "object": "model",
      "provider": "openai",
      "deployment": "hosted",
      "capabilities": ["text", "tools"]
    },
    {
      "id": "orb/private/qwen3-32b",
      "object": "model",
      "provider": "private",
      "deployment": "private",
      "capabilities": ["text"]
    }
  ]
}
```

Current implementation example with the default registry:

```bash
curl http://localhost:8080/v1/models
```

```json
{
  "object": "list",
  "data": [
    {
      "id": "orb/example-text",
      "object": "model",
      "provider": "echo",
      "deployment": "local",
      "capabilities": ["text"],
      "status": "ready"
    },
    {
      "id": "orb/private-example-text",
      "object": "model",
      "provider": "private-echo",
      "deployment": "private",
      "capabilities": ["text"],
      "status": "ready"
    }
  ]
}
```

### Responses

`POST /v1/responses` should be the primary execution endpoint.

It should accept:

- a target `model`,
- normalized `input`,
- optional execution settings,
- optional memory controls,
- optional metadata for tracing or policy.

Example request:

```json
{
  "model": "orb/gpt-4.1",
  "input": [
    {
      "role": "user",
      "content": [
        {
          "type": "input_text",
          "text": "Summarize this document in five bullets."
        }
      ]
    }
  ],
  "memory": {
    "enabled": true,
    "scope": "workspace:docs"
  },
  "metadata": {
    "request_source": "agentex-api"
  }
}
```

Example response:

```json
{
  "id": "resp_123",
  "object": "response",
  "model": "orb/gpt-4.1",
  "output": [
    {
      "type": "output_text",
      "text": "Here is the summary..."
    }
  ],
  "usage": {
    "input_tokens": 812,
    "output_tokens": 138,
    "total_tokens": 950
  },
  "runtime": {
    "adapter": "openai",
    "deployment": "hosted",
    "memory_applied": true
  }
}
```

Current implementation example with the bundled local model:

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
    ],
    "memory": {
      "enabled": true,
      "scope": "workspace:test"
    }
  }'
```

```json
{
  "id": "resp_example",
  "object": "response",
  "model": "orb/example-text",
  "output": [
    {
      "type": "output_text",
      "text": "Echo: hello orb"
    }
  ],
  "usage": {
    "input_tokens": 0,
    "output_tokens": 0,
    "total_tokens": 0
  },
  "runtime": {
    "adapter": "echo",
    "deployment": "local",
    "memory_applied": true,
    "status": "ready"
  }
}
```

Current implementation note:

- Orb accepts a top-level `stream: true` field on `POST /v1/responses`.
- Hosted OpenAI-backed models currently return server-sent events with
  `Content-Type: text/event-stream`.
- Private `private-http` routes can also return server-sent events when the
  upstream private runtime supports streaming.
- Orb currently passes through typed event names and JSON event payloads from
  the selected upstream adapter. OpenAI-backed routes currently emit event names
  such as `response.created`, `response.output_text.delta`, `response.completed`,
  and `error`.
- Streaming is adapter-specific for now. If a client requests streaming for a
  model that does not support it, Orb currently returns an SSE `error` event
  rather than switching to a JSON error body mid-stream.

Current streaming example after setting `ORB_OPENAI_API_KEY` and
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

Representative streamed event sequence:

```text
event: response.created
data: {"type":"response.created","response":{"id":"resp_stream"}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"hello"}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_stream","status":"completed"}}
```

Representative streamed private route request after setting
`ORB_PRIVATE_BASE_URL`:

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

`GET /v1/responses/{response_id}` should provide a retrieval path for runtimes
that persist response metadata or support asynchronous workflows later.

Current implementation example:

```bash
curl http://localhost:8080/v1/responses/resp_123
```

```json
{
  "error": {
    "code": "not_found",
    "message": "response \"resp_123\" is not available in the current runtime",
    "details": {
      "response_id": "resp_123",
      "persistence": "disabled"
    }
  }
}
```

The current server exposes this route as a placeholder so clients can depend on
the path shape early. Orb does not yet persist responses for later lookup.

### Memory

`POST /v1/memory/query` should expose a narrow memory retrieval path that can be
used directly when an application wants memory access without a full model
execution.

This endpoint should remain optional from an implementation standpoint, but it
is useful to reserve early because memory is part of Orb's product boundary.

Example request:

```json
{
  "scope": "workspace:docs",
  "query": "Prior decisions about private model deployment",
  "limit": 5
}
```

### Runs

`POST /v1/runs` should remain a higher-level runtime entrypoint for future
orchestrated execution.

The first implementation does not need to make `runs` feature-complete. It can
begin as a reserved endpoint or a thin wrapper around `responses` when the
runtime starts to expose more than plain model execution.

## Input Shape

The first draft should normalize request input around a message-like structure
with typed content items.

This keeps Orb compatible with:

- plain text prompts,
- future multimodal payloads,
- memory augmentation before execution,
- tool and policy metadata attached at the runtime level.

## Error Shape

The API should use a shared JSON error envelope:

```json
{
  "error": {
    "code": "invalid_argument",
    "message": "model is required",
    "details": {
      "field": "model"
    }
  }
}
```

Early error codes should cover:

- `invalid_argument`
- `not_found`
- `unauthorized`
- `forbidden`
- `rate_limited`
- `backend_unavailable`
- `internal_error`

## Deferred Decisions

The following items should be left open until implementation planning:

- authentication mechanism,
- cross-provider streaming normalization,
- exact tool-calling contract,
- persistence guarantees for response retrieval,
- memory backend schema,
- API compatibility with any external provider wire format.

## Current Status

Early implementation draft.

This API document is meant to narrow the implementation target, not to freeze a
final public contract yet.

The current repository implementation serves an early subset of this API with a
model-routed adapter registry and a bundled local `echo` adapter for the model
`orb/example-text`.

The current HTTP server also exposes `GET /v1/responses/{response_id}` as a
placeholder retrieval route. Today it returns a structured `not_found` response
because response persistence is not implemented yet.

When `ORB_OPENAI_API_KEY` and `ORB_OPENAI_MODEL_ID` are configured, the runtime
also exposes a hosted OpenAI-backed model as `orb/openai/<model-id>` by
default. That path currently forwards Orb-style `/v1/responses` calls to the
OpenAI Responses API.

That hosted OpenAI path also supports a top-level `stream: true` field on
`POST /v1/responses`. The current implementation returns server-sent events and
passes through OpenAI event names and JSON event payloads.

The default runtime also exposes a bundled private-style model,
`orb/private-example-text`, so the current skeleton already exercises model
routing across more than one deployment type.

When `ORB_PRIVATE_BASE_URL` is configured, the bundled private route is
replaced by a `private-http` adapter that forwards Orb-style `/v1/responses`
requests to an upstream private runtime. That adapter can also attach an auth
header for upstream private deployments.

If the upstream private runtime supports SSE responses, that same `private-http`
adapter also forwards top-level `stream: true` requests and passes through
upstream event names and event payloads.

If only `ORB_PRIVATE_BASE_URL` is set, the adapter uses upstream
`GET /v1/models` discovery and exposes each discovered private model as
`orb/private/<upstream-id>`.

If `ORB_PRIVATE_MODEL_ID` or `ORB_PRIVATE_UPSTREAM_MODEL` is also set, the
adapter falls back to a single explicit forwarded model and preserves the
earlier one-model mapping behavior.

In both modes, Orb includes discovery metadata for private forwarded models in
`GET /v1/models`.
