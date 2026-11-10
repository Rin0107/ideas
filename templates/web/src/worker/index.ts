// 手元の確認用の Worker (npm run e2e が wrangler dev で動かす)。本番は配信用の repo の Worker が同じヘッダーで配る。
// 静的ファイルを返し、守りのヘッダーを付けるだけ。サーバーの処理・保存は持たない (DATA.md)。
import { harden } from "./security";

export interface Env {
  ASSETS: Fetcher;
}

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    return harden(await env.ASSETS.fetch(req));
  },
} satisfies ExportedHandler<Env>;
