import fc from "fast-check";
import { describe, expect, test } from "vitest";
import { Counter, MAX_LIMIT, MAX_OPS, type Op, parseOps } from "../../src/counter";
import { runs } from "./fc";

// ---- 境界値: 範囲の端と、その 1 つ外 ----

describe("境界値", () => {
  test.each([
    [-1, false],
    [0, true],
    [1, true],
    [MAX_LIMIT - 1, true],
    [MAX_LIMIT, true],
    [MAX_LIMIT + 1, false],
    [1.5, false],
    [Number.NaN, false],
  ])("new Counter(%s) を受け付けるか: %s", (max, ok) => {
    if (ok) expect(new Counter(max).max).toBe(max);
    else expect(() => new Counter(max)).toThrow(new RangeError(`上限は 0〜${MAX_LIMIT} の整数`));
  });

  test("inc は上限ちょうどで止まる", () => {
    const c = new Counter(2);
    expect([c.inc(), c.inc(), c.inc(), c.inc()]).toEqual([true, true, false, false]);
    expect(c.value).toBe(2);
  });

  test("parseOps の長さの端", () => {
    expect(parseOps("")).toEqual([]);
    expect(parseOps("i".repeat(MAX_OPS))).toHaveLength(MAX_OPS);
    expect(() => parseOps("i".repeat(MAX_OPS + 1))).toThrow(new RangeError(`操作は ${MAX_OPS} 個まで`));
    for (const bad of ["x", "ii ", "I", "iR"])
      expect(() => parseOps(bad)).toThrow(new SyntaxError("操作は i (増やす) と r (戻す) だけ"));
  });
});

// ---- 仕様と同じ不変条件を、実装に対して確かめる (spec/Counter.cfg の INVARIANT ごとに 1 つ) ----

/** spec/Counter.tla をそのまま写したもの (Inc は n < Max のときだけ進む)。 */
function model(n: number, max: number, o: Op): number {
  if (o === "r") return 0;
  return n < max ? n + 1 : n;
}

test("Spec_TypeOK: 全件ループ (上限 0〜4・長さ 8 までの全部の操作列で、実装が仕様と同じ状態を通る)", () => {
  let checked = 0;
  const visit = (max: number, ops: Op[]) => {
    const c = new Counter(max);
    let n = 0;
    for (const o of ops) {
      c.apply([o]);
      n = model(n, max, o);
      expect(c.value).toBe(n);
      expect(c.value >= 0 && c.value <= max).toBe(true);
    }
    checked++;
  };
  const all = (max: number, prefix: Op[], depth: number) => {
    visit(max, prefix);
    if (depth === 0) return;
    for (const o of ["i", "r"] as const) all(max, [...prefix, o], depth - 1);
  };
  for (let max = 0; max <= 4; max++) all(max, [], 8);
  expect(checked).toBe(5 * 511);
});

test("Spec_Bounded: PBT (大きな上限と長い操作列で、実装と仕様を突き合わせる)", () => {
  fc.assert(
    fc.property(
      fc.integer({ min: 0, max: MAX_LIMIT }),
      fc.array(fc.constantFrom<Op>("i", "r"), { maxLength: 300 }),
      (max, ops) => {
        const c = new Counter(max);
        let n = 0;
        for (const o of ops) {
          const moved = o === "i" ? c.inc() : (c.reset(), true);
          if (o === "i") expect(moved).toBe(n < max);
          n = model(n, max, o);
          expect(c.value).toBe(n);
          expect(c.value).toBeLessThanOrEqual(max);
        }
      },
    ),
    runs(),
  );
});

test("parseOps は受け付けた文字列をそのまま操作に写す (往復)", () => {
  fc.assert(
    fc.property(fc.stringMatching(/^[ir]{0,64}$/), (s) => {
      expect(parseOps(s).join("")).toBe(s);
    }),
    runs(),
  );
});

// ---- fuzz: どんな入力でも、決めた種類のエラー以外で落ちない ----

test("fuzz: parseOps", () => {
  fc.assert(
    fc.property(fc.string({ unit: "binary", maxLength: 200 }), (s) => {
      try {
        const ops = parseOps(s);
        expect(ops.join("")).toBe(s);
        const c = new Counter(3);
        c.apply(ops);
        expect(c.value).toBeLessThanOrEqual(3);
      } catch (e) {
        expect(e instanceof RangeError || e instanceof SyntaxError).toBe(true);
      }
    }),
    runs(),
  );
});
