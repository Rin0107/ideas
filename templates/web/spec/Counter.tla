---- MODULE Counter ----
\* 例: 上限つきのカウンター (src/counter.ts) の仕様。
\* アプリを作るときは、本物の状態の流れの仕様に置き換える。
\* Counter.cfg の INVARIANT ごとに、実装を検査するテスト TestSpec_<名前> が要る (scripts/formal.sh が検査する)。
EXTENDS Naturals

CONSTANT Max
ASSUME Max \in Nat

VARIABLE n

TypeOK == n \in 0..Max
Bounded == n <= Max

Init == n = 0
Inc == /\ n < Max
       /\ n' = n + 1
Reset == n' = 0

Next == Inc \/ Reset
Spec == Init /\ [][Next]_n
====
