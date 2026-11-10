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

test("外部への通信がない", async ({ page }) => {
  const external: string[] = [];
  page.on("request", (r) => {
    const u = new URL(r.url());
    if (u.origin !== new URL(page.url() || r.url()).origin && !u.protocol.startsWith("data"))
      external.push(r.url());
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
