// @vitest-environment happy-dom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, test, vi } from "vitest";
import { App } from "../../src/App";

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
  document.body.replaceChildren();
});

function render(): HTMLElement {
  const el = document.createElement("div");
  document.body.appendChild(el);
  act(() => createRoot(el).render(<App />));
  return el;
}

test("目標と訪問回数を出し、開くたびに数える", () => {
  expect(render().querySelector('[data-testid="visits"]')?.textContent).toContain("1 回目");
  expect(render().querySelector('[data-testid="visits"]')?.textContent).toContain("2 回目");
  expect(document.querySelector("h1")?.textContent).toBe("{{NAME}}");
  expect(document.querySelector('[data-testid="goal"]')?.textContent).toBe("{{GOAL}}");
});

test("「データを消す」で、保存したものを消して読み直す", () => {
  const reload = vi.fn();
  vi.stubGlobal("location", { reload });
  const el = render();
  act(() => el.querySelector("button")?.click());
  expect(localStorage.getItem("{{NAME}}:visits")).toBeNull();
  expect(reload).toHaveBeenCalledOnce();
});
