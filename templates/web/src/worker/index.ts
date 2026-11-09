// Cloudflare Worker: 静的ファイルを返し、必要なら小さな API を持つ。
// 原則: 個人と結びつく情報は受け取らない・残さない (DATA.md)。ログに IP や本文を出さない。
import { harden } from "./security";

export interface Env {
  ASSETS: Fetcher;
}

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    const url = new URL(req.url);

    if (url.pathname === "/api/health") {
      return harden(Response.json({ ok: true }));
    }
    if (url.pathname.startsWith("/api/")) {
      return harden(Response.json({ error: "not found" }, { status: 404 }));
    }
    return harden(await env.ASSETS.fetch(req));
  },
} satisfies ExportedHandler<Env>;
