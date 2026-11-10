// 静的ファイルだけを返す。サーバーの処理 (API・保存) は持たない: アプリのデータは利用者のブラウザの中だけ。
//   /            → 一覧 (public/index.html。wk が作る)
//   /<名前>      → /<名前>/ へ移動
//   /<名前>/...  → そのアプリのファイル。ない道で HTML を求められたら、そのアプリの index.html (SPA)
import { harden } from "./security";

export interface Env {
  ASSETS: Fetcher;
}

const NAME = /^\/([a-z0-9][a-z0-9-]{0,62})(\/.*)?$/;

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    if (req.method !== "GET" && req.method !== "HEAD") {
      return harden(new Response("method not allowed", { status: 405, headers: { Allow: "GET, HEAD" } }));
    }
    const url = new URL(req.url);
    const m = NAME.exec(url.pathname);
    if (m && m[2] === undefined) {
      return harden(Response.redirect(new URL(`/${m[1]}/`, url).toString(), 308));
    }
    let res = await env.ASSETS.fetch(req);
    if (res.status === 404 && m && (req.headers.get("Accept") ?? "").includes("text/html")) {
      res = await env.ASSETS.fetch(new Request(new URL(`/${m[1]}/`, url), req));
    }
    return harden(res);
  },
} satisfies ExportedHandler<Env>;
