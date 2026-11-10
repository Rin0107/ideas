import { describe, expect, it } from "vitest";
import worker from "../../src/worker/index";
import { harden, SECURITY_HEADERS } from "../../src/worker/security";

// 守りのヘッダーは 1 文字ずつ固定する (配信用の repo の Worker と同じ値。変えるときは両方)
const EXPECTED: Record<string, string> = {
  "Content-Security-Policy":
    "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; font-src 'self'; " +
    "connect-src 'self'; media-src 'self' blob:; worker-src 'none'; manifest-src 'self'; object-src 'none'; frame-src 'none'; frame-ancestors 'none'; " +
    "base-uri 'none'; form-action 'self'; upgrade-insecure-requests",
  "X-Content-Type-Options": "nosniff",
  "Referrer-Policy": "no-referrer",
  "Strict-Transport-Security": "max-age=63072000; includeSubDomains",
  "Permissions-Policy":
    "camera=(), microphone=(), geolocation=(), payment=(), usb=(), bluetooth=(), serial=(), interest-cohort=()",
  "Cross-Origin-Opener-Policy": "same-origin",
  "Cross-Origin-Resource-Policy": "same-origin",
  "X-Frame-Options": "DENY",
};

describe("守りのヘッダー", () => {
  it("値が決めたとおり", () => {
    expect(SECURITY_HEADERS).toEqual(EXPECTED);
  });

  it("すべてのヘッダーが付き、Cookie と Server が消え、中身と状態はそのまま", async () => {
    const res = harden(new Response("x", { status: 201, headers: { "Set-Cookie": "s=1", Server: "x" } }));
    for (const [k, v] of Object.entries(EXPECTED)) expect(res.headers.get(k)).toBe(v);
    expect(res.headers.get("Set-Cookie")).toBeNull();
    expect(res.headers.get("Server")).toBeNull();
    expect(res.status).toBe(201);
    expect(await res.text()).toBe("x");
  });

  it("Worker は静的ファイルにヘッダーを付けて返す", async () => {
    const env = {
      ASSETS: {
        fetch: async (req: Request) =>
          new Response(`<p>${new URL(req.url).pathname}</p>`, { headers: { "Set-Cookie": "a=b" } }),
      },
    };
    const res = await worker.fetch(new Request("https://x.test/a/b"), env as never);
    expect(await res.text()).toBe("<p>/a/b</p>");
    expect(res.headers.get("Content-Security-Policy")).toBe(EXPECTED["Content-Security-Policy"]);
    expect(res.headers.get("Set-Cookie")).toBeNull();
  });
});
