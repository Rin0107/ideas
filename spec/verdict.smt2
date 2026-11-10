; 評価の結論 (internal/verdict.Decide) の性質。性質の否定を置き、すべて unsat なら証明できた。
; 入力: core (核が動く)・w (わくわく 1〜5)・q (品質 1〜5)・it (ブラッシュアップの回数)・mx (上限)・sw / sq (ship の基準)

(define-fun decide ((core Bool) (w Int) (q Int) (it Int) (mx Int) (sw Int) (sq Int)) Int
  ; 0 = ship, 1 = iterate, 2 = drop (verdict.Decide の switch と同じ順)
  (ite (and core (>= w sw) (>= q sq)) 0
    (ite (and core (< it mx)) 1 2)))

; 1. iterate は回数が残っているときだけ (ブラッシュアップが終わらない、がない)
(push)
(declare-const core Bool) (declare-const w Int) (declare-const q Int)
(declare-const it Int) (declare-const mx Int) (declare-const sw Int) (declare-const sq Int)
(assert (= (decide core w q it mx sw sq) 1))
(assert (>= it mx))
(check-sat)
(pop)

; 2. 核が動かないものは、点数に関係なく ship しない
(push)
(declare-const w Int) (declare-const q Int) (declare-const it Int) (declare-const mx Int)
(declare-const sw Int) (declare-const sq Int)
(assert (= (decide false w q it mx sw sq) 0))
(check-sat)
(pop)

; 3. ship なら、わくわくと品質は基準以上
(push)
(declare-const core Bool) (declare-const w Int) (declare-const q Int)
(declare-const it Int) (declare-const mx Int) (declare-const sw Int) (declare-const sq Int)
(assert (= (decide core w q it mx sw sq) 0))
(assert (or (< w sw) (< q sq)))
(check-sat)
(pop)

; 4. 結論はいつも 3 つのどれか (決まらない、がない)
(push)
(declare-const core Bool) (declare-const w Int) (declare-const q Int)
(declare-const it Int) (declare-const mx Int) (declare-const sw Int) (declare-const sq Int)
(assert (not (or (= (decide core w q it mx sw sq) 0) (= (decide core w q it mx sw sq) 1) (= (decide core w q it mx sw sq) 2))))
(check-sat)
(pop)
