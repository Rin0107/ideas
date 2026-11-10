import { expect, test } from "@playwright/test";

// 手元 (wrangler dev) とプレビュー・本番 (BASE_URL=https://…/<名前>/ を指定) の両方で流す
test("トップが開き、守りのヘッダーが付いている", async ({ page }) => {
  const res = await page.goto("./");
  expect(res?.status()).toBe(200);
  const h = res?.headers() ?? {};
  expect(h["content-security-policy"]).toContain("default-src 'self'");
  expect(h["referrer-policy"]).toBe("no-referrer");
  expect(h["set-cookie"]).toBeUndefined();
  await expect(page.getByTestId("goal")).toBeVisible();
});

test("外部への通信がない", async ({ page, baseURL }) => {
  // 自分の origin は baseURL から決める (最初の読み込みのときの page.url() は about:blank なので使わない)
  const own = new URL(baseURL ?? "http://invalid.test").origin;
  const external: string[] = [];
  page.on("request", (r) => {
    const u = new URL(r.url());
    if (u.origin !== own && u.protocol !== "data:" && u.protocol !== "blob:") external.push(r.url());
  });
  await page.goto("./");
  await page.waitForLoadState("networkidle");
  expect(external).toEqual([]);
});

test("CSP 違反がない", async ({ page }) => {
  const violations: string[] = [];
  page.on("console", (m) => {
    if (/Content Security Policy/i.test(m.text())) violations.push(m.text());
  });
  await page.goto("./");
  await page.waitForLoadState("networkidle");
  expect(violations).toEqual([]);
});

test("ホーム画面に追加できる (manifest とアイコン)", async ({ page, request }) => {
  await page.goto("./");
  const href = await page.locator('link[rel="manifest"]').getAttribute("href");
  expect(href).toBeTruthy();
  const url = new URL(href ?? "", page.url());
  const manifest = await (await request.get(url.toString())).json();
  expect(manifest.display).toBe("standalone");
  for (const icon of manifest.icons as { src: string }[]) {
    const res = await request.get(new URL(icon.src, url).toString());
    expect(res.status()).toBe(200);
  }
  const touch = await page.locator('link[rel="apple-touch-icon"]').getAttribute("href");
  expect(touch).toMatch(/\.png$/);
  expect((await request.get(new URL(touch ?? "", page.url()).toString())).status()).toBe(200);
});
