import { describe, expect, it } from "vitest";
import worker from "../../src/worker/index";
import { harden, MAX_BODY_BYTES, readJsonLimited, SECURITY_HEADERS } from "../../src/worker/security";

const env = {
  ASSETS: { fetch: async () => new Response("<html></html>", { headers: { "Set-Cookie": "a=b" } }) },
};

describe("守りのヘッダー", () => {
  it("すべてのヘッダーが付き、Cookie が消える", () => {
    const res = harden(new Response("x", { headers: { "Set-Cookie": "s=1", Server: "x" } }));
    for (const [k, v] of Object.entries(SECURITY_HEADERS)) expect(res.headers.get(k)).toBe(v);
    expect(res.headers.get("Set-Cookie")).toBeNull();
    expect(res.headers.get("Server")).toBeNull();
  });

  it("CSP に unsafe-inline / unsafe-eval / 外部の宛先がない", () => {
    const csp = SECURITY_HEADERS["Content-Security-Policy"] ?? "";
    expect(csp).not.toMatch(/unsafe-inline|unsafe-eval|https?:|\*/);
    expect(csp).toContain("frame-ancestors 'none'");
  });

  it("静的ファイルにもヘッダーが付く", async () => {
    const res = await worker.fetch(new Request("https://x.test/"), env as never);
    expect(res.headers.get("Content-Security-Policy")).toBeTruthy();
    expect(res.headers.get("Set-Cookie")).toBeNull();
  });

  it("知らない API は 404", async () => {
    const res = await worker.fetch(new Request("https://x.test/api/nope"), env as never);
    expect(res.status).toBe(404);
  });
});

describe("本文の上限", () => {
  it("上限を超えた本文は読まない", async () => {
    const big = JSON.stringify({ a: "x".repeat(MAX_BODY_BYTES) });
    expect(await readJsonLimited(new Request("https://x.test", { method: "POST", body: big }))).toBeNull();
  });
  it("壊れた JSON は null", async () => {
    expect(await readJsonLimited(new Request("https://x.test", { method: "POST", body: "{" }))).toBeNull();
  });
  it("正しい JSON は読める", async () => {
    expect(await readJsonLimited(new Request("https://x.test", { method: "POST", body: '{"a":1}' }))).toEqual(
      { a: 1 },
    );
  });
});
