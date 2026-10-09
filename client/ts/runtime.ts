export interface Options {
  /** Prepended to every path, such as "https://api.example.com". */
  baseUrl?: string;
  /** An api key, or the admin token, sent as a bearer token. */
  token?: string;
  /** The tenant the admin acts in. Nobody else sends one. */
  tenant?: string;
  /** Defaults to the global fetch. */
  fetch?: typeof fetch;
  /** "include" lets a page on another origin send the session cookie. */
  credentials?: RequestCredentials;
}

/** A response outside 2xx: its status and the server's plain text. */
export class HttpError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "HttpError";
    this.status = status;
  }
}

export interface Transport {
  json<T>(method: string, path: string, body: unknown, init?: RequestInit): Promise<T>;
  none(method: string, path: string, body: unknown, init?: RequestInit): Promise<void>;
}

export function transport(options: Options): Transport {
  const base = (options.baseUrl ?? "").replace(/\/+$/, "");
  const doFetch = options.fetch ?? globalThis.fetch.bind(globalThis);
  const defaults: Record<string, string> = {};
  if (options.token) defaults["authorization"] = "Bearer " + options.token;
  if (options.tenant) defaults["x-tenant-id"] = options.tenant;

  async function send(method: string, path: string, body: unknown, init?: RequestInit): Promise<Response> {
    const headers = new Headers(init?.headers);
    for (const key in defaults) if (!headers.has(key)) headers.set(key, defaults[key]!);
    const req: RequestInit = { ...init, method, headers };
    if (options.credentials && !init?.credentials) req.credentials = options.credentials;
    if (body !== undefined) {
      headers.set("content-type", "application/json");
      req.body = JSON.stringify(body);
    }
    const res = await doFetch(base + path, req);
    if (!res.ok) throw new HttpError(res.status, (await res.text()).trim());
    return res;
  }

  return {
    async json<T>(method: string, path: string, body: unknown, init?: RequestInit): Promise<T> {
      return (await (await send(method, path, body, init)).json()) as T;
    },
    async none(method: string, path: string, body: unknown, init?: RequestInit): Promise<void> {
      await (await send(method, path, body, init)).body?.cancel();
    },
  };
}
