import assert from "node:assert/strict";
import { test } from "node:test";

import { createClient, HttpError, type Options, type User } from "./client.ts";

const guy: User = { id: "AZmQDbF0dYKmUv1bTGYBAw", name: "Guy", email: "guy@acme.io", created_at: "2026-10-06T12:00:00Z" };

interface Seen {
  url: string;
  method: string;
  headers: Record<string, string>;
  body: string | undefined;
  credentials: RequestCredentials | undefined;
}

// serve answers every request with res and records what was sent.
function serve(res: () => Response, options: Options = {}) {
  const seen: Seen[] = [];
  const client = createClient({
    baseUrl: "https://api.test/",
    ...options,
    fetch: async (input, init) => {
      seen.push({
        url: String(input),
        method: init?.method ?? "",
        headers: Object.fromEntries(new Headers(init?.headers)),
        body: init?.body as string | undefined,
        credentials: init?.credentials,
      });
      return res();
    },
  });
  return { client, seen };
}

const json = (v: unknown) => () => Response.json(v);

test("get: path, credentials, decoded result", async () => {
  const { client, seen } = serve(json(guy), { token: "gsk_abc", tenant: "AZmQDbF0dYKmUv1bTGYAAA" });

  assert.deepEqual(await client.users.get(guy.id), guy);
  assert.deepEqual(seen, [{
    url: "https://api.test/api/users/AZmQDbF0dYKmUv1bTGYBAw",
    method: "GET",
    headers: { authorization: "Bearer gsk_abc", "x-tenant-id": "AZmQDbF0dYKmUv1bTGYAAA" },
    body: undefined,
    credentials: undefined,
  }]);
});

test("query: method and json body", async () => {
  const { client, seen } = serve(json([guy, guy]));

  assert.deepEqual(await client.users.query({ name_like: "gu", sort: "name", page: { limit: 10 } }), [guy, guy]);
  assert.equal(seen[0]?.method, "QUERY");
  assert.equal(seen[0]?.headers["content-type"], "application/json");
  assert.equal(seen[0]?.body, `{"name_like":"gu","sort":"name","page":{"limit":10}}`);
});

test("void route resolves to undefined", async () => {
  const { client, seen } = serve(() => new Response(null, { status: 200 }));

  assert.equal(await client.posts.delete(guy.id), undefined);
  assert.equal(seen[0]?.method, "DELETE");
  assert.equal(seen[0]?.url, "https://api.test/api/posts/AZmQDbF0dYKmUv1bTGYBAw");
});

test("nested path", async () => {
  const { client, seen } = serve(json([]));

  await client.posts.listByAuthor(guy.id);
  assert.equal(seen[0]?.url, "https://api.test/api/users/AZmQDbF0dYKmUv1bTGYBAw/posts");
});

test("path params are escaped", async () => {
  const { client, seen } = serve(json(guy));

  await client.users.get("a/b?c");
  assert.equal(seen[0]?.url, "https://api.test/api/users/a%2Fb%3Fc");
});

test("init headers win over the defaults", async () => {
  const { client, seen } = serve(json([]), { token: "gsk_abc", credentials: "include" });

  await client.users.list({ headers: { authorization: "Bearer other" } });
  assert.equal(seen[0]?.headers["authorization"], "Bearer other");
  assert.equal(seen[0]?.credentials, "include");
});

test("error: status and trimmed text", async () => {
  const { client } = serve(() => new Response("tenant not found\n", { status: 404 }));

  await assert.rejects(client.auth.me(), (err: unknown) => {
    assert.ok(err instanceof HttpError);
    assert.equal(err.status, 404);
    assert.equal(err.message, "tenant not found");
    return true;
  });
});

// compile-time only: tsc fails if any of these start type-checking
async function typing() {
  const c = createClient();
  // @ts-expect-error ids are strings
  await c.users.get(1);
  // @ts-expect-error a required field of the body is missing
  await c.users.create({ name: "Guy", email: "guy@acme.io" });
  // @ts-expect-error sort is a closed set
  await c.users.query({ sort: "password" });
  // @ts-expect-error a delete answers nothing
  const nothing: User = await c.users.delete("x");
  const users: User[] = await c.users.list();
  return { nothing, users };
}
void typing;
