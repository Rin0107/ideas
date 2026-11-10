// @vitest-environment happy-dom
import { act } from "react";
import { beforeEach, expect, test, vi } from "vitest";

beforeEach(() => {
  vi.resetModules();
  document.body.replaceChildren();
});

test("#root に描く", async () => {
  const root = document.createElement("div");
  root.id = "root";
  document.body.appendChild(root);
  await act(async () => {
    await import("../../src/main");
  });
  expect(document.querySelector('[data-testid="goal"]')).not.toBeNull();
});

test("#root がなければ止まる", async () => {
  await expect(import("../../src/main")).rejects.toThrow("#root がない");
});
