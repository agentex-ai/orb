# Agentex Orb TypeScript SDK

This directory contains a minimal zero-dependency TypeScript SDK skeleton for
the current Agentex Orb HTTP API.

It currently covers:

- `GET /v1/models`
- `POST /v1/responses`
- `GET /v1/responses/{response_id}`
- `POST /v1/memory/query`
- `POST /v1/runs`
- SSE streaming helpers for `responses` and `runs`

## Status

Early SDK skeleton.

This client is designed to mirror the current Orb runtime surface, including
its early-stage behavior:

- response retrieval is in-memory per running Orb process,
- memory query is in-memory per running Orb process,
- streaming paths return upstream-typed SSE events.

## Example

```ts
import { OrbClient } from "agentex-orb-sdk";

const client = new OrbClient({ baseUrl: "http://localhost:8080" });

const models = await client.listModels();
console.log(models.data.map((model) => model.id));

const response = await client.createResponse({
  model: "orb/example-text",
  input: [
    {
      role: "user",
      content: [{ type: "input_text", text: "hello orb" }]
    }
  ]
});

console.log(response.output[0]?.text);

for await (const event of client.streamResponse({
  model: "orb/openai/gpt-5-mini",
  stream: true,
  input: [
    {
      role: "user",
      content: [{ type: "input_text", text: "write one short line" }]
    }
  ]
})) {
  console.log(event.type, event.data);
}
```

## Development

```bash
npm install
npm run typecheck
```
