; 例: Counter.Inc は 0 <= n <= max を保つ (internal/app/counter.go)。
; 決まり: 証明したい性質の「否定」を assert し、(check-sat) が unsat なら証明できた。
; scripts/formal.sh は、すべての (check-sat) が unsat であることを求める。

; 1. Inc は範囲を保つ (どんな整数 n・max でも)
(push)
(declare-const n Int)
(declare-const mx Int)
(declare-const n2 Int)
(assert (and (<= 0 n) (<= n mx)))
(assert (= n2 (ite (>= n mx) n (+ n 1))))
(assert (not (and (<= 0 n2) (<= n2 mx))))
(check-sat)
(pop)

; 2. NewCounter が受け付ける上限なら、0 は範囲の中 (初期状態が TypeOK)
(push)
(declare-const mx Int)
(assert (and (>= mx 0) (<= mx 1000)))
(assert (not (and (<= 0 0) (<= 0 mx))))
(check-sat)
(pop)
