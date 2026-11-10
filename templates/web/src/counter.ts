// 例: 上限つきのカウンター。仕様は spec/Counter.tla (状態の流れ) と spec/counter.smt2 (算術の性質)。
// アプリを作るときは、この例 (src/counter.ts・tests/unit/counter.test.ts・spec/Counter.*・spec/counter.smt2) を消して本物に置き換える。
// 形は同じにする: 仕様を書く → 実装する → 仕様の不変条件ごとに "Spec_<名前>: …" のテストで実装を検査する。

/** カウンターの上限に指定できる最大値。 */
export const MAX_LIMIT = 1000;
/** 1 回に受け付ける操作の数の上限 (入力の大きさに上限をかける)。 */
export const MAX_OPS = 10_000;

export type Op = "i" | "r";

/** 0 から max までの値を持つ。仕様の変数 n に当たる。 */
export class Counter {
  #n = 0;
  readonly max: number;

  /** 仕様の Init に当たる。上限が範囲 [0, MAX_LIMIT] の整数でなければ投げる。 */
  constructor(max: number) {
    if (!Number.isInteger(max) || max < 0 || max > MAX_LIMIT) {
      throw new RangeError(`上限は 0〜${MAX_LIMIT} の整数`);
    }
    this.max = max;
  }

  /** 上限未満なら 1 増やして true。上限なら何もせず false (仕様では足踏み)。仕様の Inc に当たる。 */
  inc(): boolean {
    if (this.#n >= this.max) return false;
    this.#n++;
    return true;
  }

  /** 0 に戻す。仕様の Reset に当たる。 */
  reset(): void {
    this.#n = 0;
  }

  get value(): number {
    return this.#n;
  }

  apply(ops: readonly Op[]): void {
    for (const o of ops) {
      if (o === "i") this.inc();
      else this.reset();
    }
  }
}

/** "iir" のような文字列を操作の列にする。i と r 以外、または長すぎれば投げる。 */
export function parseOps(s: string): Op[] {
  if (s.length > MAX_OPS) throw new RangeError(`操作は ${MAX_OPS} 個まで`);
  return [...s].map((c) => {
    if (c !== "i" && c !== "r") throw new SyntaxError("操作は i (増やす) と r (戻す) だけ");
    return c;
  });
}
