from __future__ import annotations

from dataclasses import dataclass
import json
from typing import Any, Iterator, Mapping, NotRequired, TypedDict
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


class Model(TypedDict):
    id: str
    object: str
    provider: str
    deployment: str
    capabilities: list[str]
    status: NotRequired[str]
    metadata: NotRequired[dict[str, Any]]


class ModelList(TypedDict):
    object: str
    data: list[Model]


class InputContent(TypedDict):
    type: str
    text: NotRequired[str]


class InputMessage(TypedDict):
    role: str
    content: list[InputContent]


class MemoryRequest(TypedDict):
    enabled: bool
    scope: NotRequired[str]


class ResponseRequest(TypedDict):
    model: str
    input: list[InputMessage]
    memory: NotRequired[MemoryRequest]
    stream: NotRequired[bool]
    metadata: NotRequired[dict[str, Any]]
    settings: NotRequired[dict[str, Any]]


class OutputItem(TypedDict):
    type: str
    text: str


class Usage(TypedDict):
    input_tokens: int
    output_tokens: int
    total_tokens: int


class Runtime(TypedDict):
    adapter: str
    deployment: str
    memory_applied: bool
    status: str


class ResponseEnvelope(TypedDict):
    id: str
    object: str
    model: str
    output: list[OutputItem]
    usage: Usage
    runtime: Runtime


class MemoryQueryRequest(TypedDict):
    scope: str
    query: NotRequired[str]
    limit: NotRequired[int]


class MemoryItem(TypedDict):
    id: str
    object: str
    scope: str
    response_id: str
    model: str
    input_text: NotRequired[str]
    output_text: NotRequired[str]


class MemoryQueryResponse(TypedDict):
    object: str
    data: list[MemoryItem]


class APIError(TypedDict):
    code: str
    message: str
    details: NotRequired[dict[str, Any]]


class ErrorEnvelope(TypedDict):
    error: APIError


@dataclass(frozen=True)
class StreamEvent:
    type: str
    data: Any


class OrbAPIError(Exception):
    def __init__(self, error: APIError, status: int) -> None:
        super().__init__(error["message"])
        self.code = error["code"]
        self.status = status
        self.details = error.get("details")


class OrbClient:
    def __init__(
        self,
        *,
        base_url: str = "http://localhost:8080",
        api_key: str | None = None,
        headers: Mapping[str, str] | None = None,
        timeout: float | None = None,
    ) -> None:
        self._base_url = _trim_trailing_slash(base_url)
        self._api_key = _trim_string(api_key)
        self._headers = dict(headers or {})
        self._timeout = timeout

    def list_models(
        self, *, headers: Mapping[str, str] | None = None, timeout: float | None = None
    ) -> ModelList:
        return self._request_json("/v1/models", method="GET", headers=headers, timeout=timeout)

    def create_response(
        self,
        request: ResponseRequest,
        *,
        headers: Mapping[str, str] | None = None,
        timeout: float | None = None,
    ) -> ResponseEnvelope:
        return self._request_json(
            "/v1/responses", method="POST", body=request, headers=headers, timeout=timeout
        )

    def stream_response(
        self,
        request: ResponseRequest,
        *,
        headers: Mapping[str, str] | None = None,
        timeout: float | None = None,
    ) -> Iterator[StreamEvent]:
        yield from self._stream_request("/v1/responses", request, headers=headers, timeout=timeout)

    def get_response(
        self,
        response_id: str,
        *,
        headers: Mapping[str, str] | None = None,
        timeout: float | None = None,
    ) -> ResponseEnvelope:
        return self._request_json(
            f"/v1/responses/{response_id}",
            method="GET",
            headers=headers,
            timeout=timeout,
        )

    def query_memory(
        self,
        query: MemoryQueryRequest,
        *,
        headers: Mapping[str, str] | None = None,
        timeout: float | None = None,
    ) -> MemoryQueryResponse:
        return self._request_json(
            "/v1/memory/query", method="POST", body=query, headers=headers, timeout=timeout
        )

    def create_run(
        self,
        request: ResponseRequest,
        *,
        headers: Mapping[str, str] | None = None,
        timeout: float | None = None,
    ) -> ResponseEnvelope:
        return self._request_json("/v1/runs", method="POST", body=request, headers=headers, timeout=timeout)

    def stream_run(
        self,
        request: ResponseRequest,
        *,
        headers: Mapping[str, str] | None = None,
        timeout: float | None = None,
    ) -> Iterator[StreamEvent]:
        yield from self._stream_request("/v1/runs", request, headers=headers, timeout=timeout)

    def _request_json(
        self,
        path: str,
        *,
        method: str,
        body: dict[str, Any] | None = None,
        headers: Mapping[str, str] | None = None,
        timeout: float | None = None,
    ) -> Any:
        request = self._build_request(path, method=method, body=body, headers=headers)
        response = self._open(request, timeout=timeout)
        try:
            payload = response.read()
            return _decode_json_payload(payload, status=response.status)
        finally:
            response.close()

    def _stream_request(
        self,
        path: str,
        request_body: ResponseRequest,
        *,
        headers: Mapping[str, str] | None = None,
        timeout: float | None = None,
    ) -> Iterator[StreamEvent]:
        payload = dict(request_body)
        payload["stream"] = True
        request = self._build_request(
            path,
            method="POST",
            body=payload,
            headers=headers,
            wants_stream=True,
        )
        response = self._open(request, timeout=timeout)
        try:
            yield from _parse_event_stream(response)
        finally:
            response.close()

    def _build_request(
        self,
        path: str,
        *,
        method: str,
        body: dict[str, Any] | None = None,
        headers: Mapping[str, str] | None = None,
        wants_stream: bool = False,
    ) -> Request:
        payload = None if body is None else json.dumps(body).encode("utf-8")
        request_headers = self._build_headers(
            headers=headers,
            has_json_body=payload is not None,
            wants_stream=wants_stream,
        )
        return Request(
            f"{self._base_url}{path}",
            data=payload,
            headers=request_headers,
            method=method,
        )

    def _build_headers(
        self,
        *,
        headers: Mapping[str, str] | None,
        has_json_body: bool,
        wants_stream: bool,
    ) -> dict[str, str]:
        request_headers = dict(self._headers)
        if self._api_key:
            request_headers["Authorization"] = f"Bearer {self._api_key}"
        if has_json_body and "Content-Type" not in request_headers:
            request_headers["Content-Type"] = "application/json"
        if wants_stream:
            request_headers["Accept"] = "text/event-stream"
        if headers:
            request_headers.update(headers)
        return request_headers

    def _open(self, request: Request, *, timeout: float | None) -> Any:
        try:
            return urlopen(request, timeout=self._timeout if timeout is None else timeout)
        except HTTPError as error:
            raise _orb_api_error_from_http_error(error) from None
        except URLError as error:
            raise OrbAPIError(
                {
                    "code": "connection_error",
                    "message": f"failed to reach Orb: {error.reason}",
                },
                0,
            ) from error


def _trim_trailing_slash(value: str) -> str:
    trimmed = value.rstrip("/")
    return trimmed or value


def _trim_string(value: str | None) -> str | None:
    if value is None:
        return None
    trimmed = value.strip()
    return trimmed or None


def _decode_json_payload(payload: bytes, *, status: int) -> Any:
    if not payload:
        return {}
    try:
        return json.loads(payload.decode("utf-8"))
    except json.JSONDecodeError as error:
        raise OrbAPIError(
            {
                "code": "invalid_response",
                "message": "Orb returned invalid JSON",
                "details": {"decode_error": str(error)},
            },
            status,
        ) from error


def _orb_api_error_from_http_error(error: HTTPError) -> OrbAPIError:
    body = error.read()
    content_type = error.headers.get("Content-Type", "")
    if "application/json" in content_type:
        try:
            payload = json.loads(body.decode("utf-8"))
        except json.JSONDecodeError:
            payload = None
        if isinstance(payload, dict):
            details = payload.get("error")
            if isinstance(details, dict):
                code = details.get("code")
                message = details.get("message")
                if isinstance(code, str) and isinstance(message, str):
                    return OrbAPIError(
                        {
                            "code": code,
                            "message": message,
                            "details": details.get("details"),
                        },
                        error.code,
                    )

    text = body.decode("utf-8", errors="replace").strip()
    return OrbAPIError(
        {
            "code": "http_error",
            "message": text or f"Orb request failed with status {error.code}",
        },
        error.code,
    )


def _parse_event_stream(response: Any) -> Iterator[StreamEvent]:
    block_lines: list[str] = []

    while True:
        raw_line = response.readline()
        if not raw_line:
            break

        line = raw_line.decode("utf-8").rstrip("\r\n")
        if line == "":
            event = _parse_event_block(block_lines)
            if event is not None:
                yield event
            block_lines = []
            continue

        block_lines.append(line)

    if block_lines:
        event = _parse_event_block(block_lines)
        if event is not None:
            yield event


def _parse_event_block(lines: list[str]) -> StreamEvent | None:
    event_type = "message"
    data_lines: list[str] = []

    for line in lines:
        if not line or line.startswith(":"):
            continue
        if line.startswith("event:"):
            event_type = line[len("event:") :].strip() or "message"
            continue
        if line.startswith("data:"):
            data_lines.append(line[len("data:") :].strip())

    if not data_lines and event_type == "message":
        return None

    raw_data = "\n".join(data_lines).strip() or "{}"
    if raw_data == "[DONE]":
        event_type = "done"
        raw_data = "{}"

    try:
        data = json.loads(raw_data)
    except json.JSONDecodeError:
        data = {"raw": raw_data}

    return StreamEvent(type=event_type, data=data)
