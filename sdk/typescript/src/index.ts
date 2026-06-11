export interface ClientOptions {
  baseUrl?: string;
  apiKey?: string;
  headers?: HeadersInit;
  fetch?: typeof fetch;
}

export interface RequestOptions {
  signal?: AbortSignal;
  headers?: HeadersInit;
}

export interface Model {
  id: string;
  object: string;
  provider: string;
  deployment: string;
  capabilities: string[];
  status?: string;
  metadata?: Record<string, unknown>;
}

export interface ModelList {
  object: string;
  data: Model[];
}

export interface InputContent {
  type: string;
  text?: string;
}

export interface InputMessage {
  role: string;
  content: InputContent[];
}

export interface MemoryRequest {
  enabled: boolean;
  scope?: string;
}

export interface ResponseRequest {
  model: string;
  input: InputMessage[];
  memory?: MemoryRequest;
  stream?: boolean;
  metadata?: Record<string, unknown>;
  settings?: Record<string, unknown>;
}

export interface OutputItem {
  type: string;
  text: string;
}

export interface Usage {
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
}

export interface Runtime {
  adapter: string;
  deployment: string;
  memory_applied: boolean;
  status: string;
}

export interface ResponseEnvelope {
  id: string;
  object: string;
  model: string;
  output: OutputItem[];
  usage: Usage;
  runtime: Runtime;
}

export interface MemoryQueryRequest {
  scope: string;
  query?: string;
  limit?: number;
}

export interface MemoryItem {
  id: string;
  object: string;
  scope: string;
  response_id: string;
  model: string;
  input_text?: string;
  output_text?: string;
}

export interface MemoryQueryResponse {
  object: string;
  data: MemoryItem[];
}

export interface APIError {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}

export interface ErrorEnvelope {
  error: APIError;
}

export interface StreamEvent<T = unknown> {
  type: string;
  data: T;
}

export class OrbAPIError extends Error {
  readonly code: string;
  readonly status: number;
  readonly details?: Record<string, unknown>;

  constructor(error: APIError, status: number) {
    super(error.message);
    this.name = "OrbAPIError";
    this.code = error.code;
    this.status = status;
    this.details = error.details;
  }
}

export class OrbClient {
  private readonly baseUrl: string;
  private readonly apiKey?: string;
  private readonly defaultHeaders?: HeadersInit;
  private readonly fetchImpl: typeof fetch;

  constructor(options: ClientOptions = {}) {
    this.baseUrl = trimTrailingSlash(options.baseUrl ?? "http://localhost:8080");
    this.apiKey = trimString(options.apiKey);
    this.defaultHeaders = options.headers;
    this.fetchImpl = options.fetch ?? getDefaultFetch();
  }

  async listModels(options: RequestOptions = {}): Promise<ModelList> {
    return this.requestJson<ModelList>("/v1/models", {
      method: "GET",
      signal: options.signal,
      headers: options.headers
    });
  }

  async createResponse(request: ResponseRequest, options: RequestOptions = {}): Promise<ResponseEnvelope> {
    return this.requestJson<ResponseEnvelope>("/v1/responses", {
      method: "POST",
      body: request,
      signal: options.signal,
      headers: options.headers
    });
  }

  async *streamResponse(
    request: ResponseRequest,
    options: RequestOptions = {}
  ): AsyncGenerator<StreamEvent, void, unknown> {
    yield* this.streamRequest("/v1/responses", request, options);
  }

  async getResponse(responseId: string, options: RequestOptions = {}): Promise<ResponseEnvelope> {
    return this.requestJson<ResponseEnvelope>(`/v1/responses/${encodeURIComponent(responseId)}`, {
      method: "GET",
      signal: options.signal,
      headers: options.headers
    });
  }

  async queryMemory(query: MemoryQueryRequest, options: RequestOptions = {}): Promise<MemoryQueryResponse> {
    return this.requestJson<MemoryQueryResponse>("/v1/memory/query", {
      method: "POST",
      body: query,
      signal: options.signal,
      headers: options.headers
    });
  }

  async createRun(request: ResponseRequest, options: RequestOptions = {}): Promise<ResponseEnvelope> {
    return this.requestJson<ResponseEnvelope>("/v1/runs", {
      method: "POST",
      body: request,
      signal: options.signal,
      headers: options.headers
    });
  }

  async *streamRun(
    request: ResponseRequest,
    options: RequestOptions = {}
  ): AsyncGenerator<StreamEvent, void, unknown> {
    yield* this.streamRequest("/v1/runs", request, options);
  }

  private async requestJson<T>(
    path: string,
    options: {
      method: string;
      body?: unknown;
      signal?: AbortSignal;
      headers?: HeadersInit;
    }
  ): Promise<T> {
    const response = await this.fetchImpl(`${this.baseUrl}${path}`, {
      method: options.method,
      headers: this.buildHeaders(options.headers, options.body !== undefined, false),
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal
    });

    if (!response.ok) {
      await throwAPIError(response);
    }

    return (await response.json()) as T;
  }

  private async *streamRequest(
    path: string,
    request: ResponseRequest,
    options: RequestOptions
  ): AsyncGenerator<StreamEvent, void, unknown> {
    const response = await this.fetchImpl(`${this.baseUrl}${path}`, {
      method: "POST",
      headers: this.buildHeaders(options.headers, true, true),
      body: JSON.stringify({ ...request, stream: true }),
      signal: options.signal
    });

    if (!response.ok) {
      await throwAPIError(response);
    }

    if (!response.body) {
      throw new Error("Orb streaming response body is empty");
    }

    for await (const event of parseEventStream(response.body)) {
      yield event;
    }
  }

  private buildHeaders(extra: HeadersInit | undefined, hasJSONBody: boolean, wantsStream: boolean): Headers {
    const headers = new Headers(this.defaultHeaders);
    if (this.apiKey) {
      headers.set("Authorization", `Bearer ${this.apiKey}`);
    }
    if (hasJSONBody && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
    if (wantsStream) {
      headers.set("Accept", "text/event-stream");
    }
    if (extra) {
      new Headers(extra).forEach((value, key) => headers.set(key, value));
    }
    return headers;
  }
}

function getDefaultFetch(): typeof fetch {
  if (typeof fetch !== "function") {
    throw new Error("OrbClient requires a fetch implementation");
  }
  return fetch.bind(globalThis);
}

function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, "");
}

function trimString(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed : undefined;
}

async function throwAPIError(response: Response): Promise<never> {
  const contentType = response.headers.get("content-type") ?? "";
  if (contentType.includes("application/json")) {
    try {
      const payload = (await response.json()) as ErrorEnvelope;
      if (payload?.error?.code && payload.error.message) {
        throw new OrbAPIError(payload.error, response.status);
      }
    } catch (error) {
      if (error instanceof OrbAPIError) {
        throw error;
      }
    }
  }

  const body = await response.text();
  throw new OrbAPIError(
    {
      code: "http_error",
      message: body || `Orb request failed with status ${response.status}`
    },
    response.status
  );
}

async function* parseEventStream(
  stream: ReadableStream<Uint8Array>
): AsyncGenerator<StreamEvent, void, unknown> {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) {
        buffer += decoder.decode();
        break;
      }

      buffer += decoder.decode(value, { stream: true });
      const parts = buffer.split(/\r?\n\r?\n/);
      buffer = parts.pop() ?? "";
      for (const block of parts) {
        const event = parseEventBlock(block);
        if (event) {
          yield event;
        }
      }
    }

    if (buffer.trim() !== "") {
      const event = parseEventBlock(buffer);
      if (event) {
        yield event;
      }
    }
  } finally {
    reader.releaseLock();
  }
}

function parseEventBlock(block: string): StreamEvent | null {
  const lines = block.split(/\r?\n/);
  let eventType = "message";
  const dataLines: string[] = [];

  for (const line of lines) {
    if (!line || line.startsWith(":")) {
      continue;
    }

    if (line.startsWith("event:")) {
      eventType = line.slice("event:".length).trim() || "message";
      continue;
    }

    if (line.startsWith("data:")) {
      dataLines.push(line.slice("data:".length).trim());
    }
  }

  if (dataLines.length === 0 && eventType === "message") {
    return null;
  }

  let rawData = dataLines.join("\n").trim();
  if (rawData === "") {
    rawData = "{}";
  }
  if (rawData === "[DONE]") {
    eventType = "done";
    rawData = "{}";
  }

  return {
    type: eventType,
    data: parseEventData(rawData)
  };
}

function parseEventData(rawData: string): unknown {
  try {
    return JSON.parse(rawData) as unknown;
  } catch {
    return { raw: rawData };
  }
}
