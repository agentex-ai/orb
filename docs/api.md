# API Draft

This document outlines the first public API shape for Agentex Orb.

The goal of this draft is to define a small, stable surface that can support:

- model discovery,
- request execution,
- optional memory-backed execution,
- future runtime metadata and policy hooks.

This is still a planning document. Field names and wire contracts may evolve as
the first implementation is shaped.

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

`GET /v1/responses/{response_id}` should provide a retrieval path for runtimes
that persist response metadata or support asynchronous workflows later.

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
- streaming response format,
- exact tool-calling contract,
- persistence guarantees for response retrieval,
- memory backend schema,
- API compatibility with any external provider wire format.

## Current Status

Early draft only.

This API document is meant to narrow the first implementation target, not to
freeze a final public contract yet.

The current repository implementation serves an early subset of this API with a
model-routed adapter registry and a bundled local `echo` adapter for the model
`orb/example-text`.

The default runtime also exposes a bundled private-style model,
`orb/private-example-text`, so the current skeleton already exercises model
routing across more than one deployment type.

When `ORB_PRIVATE_BASE_URL` is configured, the default private model route is
served by a `private-http` adapter that forwards Orb-style `/v1/responses`
requests to an upstream private runtime.
