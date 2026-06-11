# Agentex Orb Python SDK

This directory contains a minimal zero-dependency Python SDK skeleton for the
current Agentex Orb HTTP API.

It currently covers:

- `GET /v1/models`
- `POST /v1/responses`
- `GET /v1/responses/{response_id}`
- `POST /v1/memory/query`
- `POST /v1/runs`
- SSE streaming helpers for `responses` and `runs`

## Status

Early SDK skeleton.

This client mirrors the current Orb runtime surface, including its early-stage
behavior:

- response retrieval is in-memory per running Orb process,
- memory query is in-memory per running Orb process,
- streaming paths return upstream-typed SSE events.

## Example

```python
from agentex_orb import OrbClient

client = OrbClient(base_url="http://localhost:8080")

models = client.list_models()
print([model["id"] for model in models["data"]])

response = client.create_response(
    {
        "model": "orb/example-text",
        "input": [
            {
                "role": "user",
                "content": [{"type": "input_text", "text": "hello orb"}],
            }
        ],
    }
)

print(response["output"][0]["text"])

for event in client.stream_response(
    {
        "model": "orb/openai/gpt-5-mini",
        "stream": True,
        "input": [
            {
                "role": "user",
                "content": [{"type": "input_text", "text": "write one short line"}],
            }
        ],
    }
):
    print(event.type, event.data)
```

## Development

```bash
python -m pip install -e .
```
