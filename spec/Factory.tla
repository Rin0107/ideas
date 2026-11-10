---- MODULE Factory ----
\* 工場の流れの仕様 (internal/next.Decide・internal/verdict.Decide・cmd/wk の各段階)。
\* 作っているアプリは同時に 1 つ (WIP = 1)。retro・intake は状態を変えないので省く。
\* Factory.cfg の INVARIANT ごとに、実装を検査する Go のテスト TestSpec_<名前> がある (scripts/formal.sh が検査)。
EXTENDS Naturals

CONSTANTS MaxTicksMVP, MaxTicksIteration, MaxIterations, MaxIssues, MaxProposals

Statuses == {"none", "selected", "building", "evaluating", "iterating", "shipped", "dropped"}
Verdicts == {"", "ship", "iterate", "drop"}

\* proposals: ロールの見回りが出した、まだ仕分けていない提案の数 (docs/ROLES.md)
VARIABLES status, verdict, iteration, phaseTicks, open, blocked, shipped, dropped, proposals
vars == <<status, verdict, iteration, phaseTicks, open, blocked, shipped, dropped, proposals>>

Limit == IF status = "iterating" THEN MaxTicksIteration ELSE MaxTicksMVP
Working == status \in {"building", "iterating"}

TypeOK ==
  /\ status \in Statuses
  /\ verdict \in Verdicts
  /\ iteration \in 0..MaxIterations
  /\ phaseTicks \in 0..MaxTicksMVP
  /\ open \in 0..MaxIssues
  /\ blocked \in 0..open
  /\ shipped \in 0..1
  /\ dropped \in 0..1
  /\ proposals \in 0..MaxProposals

\* 評価の結論は、評価した直後にだけある (結論を持ったまま作り続けない)
VerdictOnlyWhenEvaluated == verdict # "" => status = "evaluating"
\* 作業は tick の上限を超えない (超えたら必ず評価に回る)
NoBuildPastLimit == Working => phaseTicks <= Limit
\* ブラッシュアップは上限の回数まで
IterationBound == iteration <= MaxIterations
\* 公開と破棄は終わりの状態で、すぐ次のアプリに移る (終わったアプリを抱えない)
FinishedIsTransient == status \notin {"shipped", "dropped"}

Init ==
  /\ status = "none" /\ verdict = "" /\ iteration = 0 /\ phaseTicks = 0
  /\ open = 0 /\ blocked = 0 /\ shipped = 0 /\ dropped = 0 /\ proposals = 0

\* select: アイディアを選び、repo と MVP の issue を作る (2 段階: 選定 → 作成)
Select ==
  /\ status = "none"
  /\ status' = "selected"
  /\ UNCHANGED <<verdict, iteration, phaseTicks, open, blocked, shipped, dropped, proposals>>
Scaffold ==
  /\ status = "selected"
  /\ \E n \in 1..MaxIssues : open' = n
  /\ status' = "building" /\ phaseTicks' = 0 /\ blocked' = 0 /\ iteration' = 0
  /\ UNCHANGED <<verdict, shipped, dropped, proposals>>

\* build: 1 tick で issue を 1 つ。閉じるか、試行が尽きて blocked になるか、失敗して残る
Build ==
  /\ Working /\ phaseTicks < Limit /\ open > blocked /\ proposals = 0
  /\ phaseTicks' = phaseTicks + 1
  /\ \/ open' = open - 1 /\ blocked' = blocked
     \/ blocked' = blocked + 1 /\ open' = open
     \/ UNCHANGED <<open, blocked>>
  /\ UNCHANGED <<status, verdict, iteration, shipped, dropped, proposals>>

\* patrol: ロールが見回って、提案を 0 個以上出す。triage: leader が提案を仕分け、今やるものだけ issue に加える。
\* どちらも tick を使う (だから時間切れは必ず来る)。提案があるあいだは、build より先に仕分ける
Patrol ==
  /\ Working /\ phaseTicks < Limit /\ proposals = 0
  /\ phaseTicks' = phaseTicks + 1
  /\ \E n \in 0..MaxProposals : proposals' = n
  /\ UNCHANGED <<status, verdict, iteration, open, blocked, shipped, dropped>>
Triage ==
  /\ Working /\ phaseTicks < Limit /\ proposals > 0
  /\ phaseTicks' = phaseTicks + 1
  /\ \E k \in 0..proposals : open + k <= MaxIssues /\ open' = open + k
  /\ proposals' = 0
  /\ UNCHANGED <<status, verdict, iteration, blocked, shipped, dropped>>

\* 時間切れ・全部閉じた・全部 blocked なら評価へ
ToEvaluate ==
  /\ Working /\ (phaseTicks >= Limit \/ (open = blocked /\ proposals = 0))
  /\ status' = "evaluating"
  /\ UNCHANGED <<verdict, iteration, phaseTicks, open, blocked, shipped, dropped, proposals>>

\* evaluate: 結論は点数で決まる。iterate はブラッシュアップの回数が残っているときだけ
Evaluate ==
  /\ status = "evaluating" /\ verdict = ""
  /\ \E v \in {"ship", "drop"} \cup (IF iteration < MaxIterations THEN {"iterate"} ELSE {}) : verdict' = v
  /\ UNCHANGED <<status, iteration, phaseTicks, open, blocked, shipped, dropped, proposals>>

Iterate ==
  /\ verdict = "iterate"
  /\ \E n \in 1..MaxIssues : open' = n
  /\ status' = "iterating" /\ verdict' = "" /\ iteration' = iteration + 1 /\ phaseTicks' = 0 /\ blocked' = 0
  /\ UNCHANGED <<shipped, dropped, proposals>>

\* ship・drop: 記録して、次のアプリへ (status は none に戻る)
Ship ==
  /\ verdict = "ship"
  /\ shipped' = 1 - shipped
  /\ status' = "none" /\ verdict' = "" /\ iteration' = 0 /\ phaseTicks' = 0 /\ open' = 0 /\ blocked' = 0 /\ proposals' = 0
  /\ UNCHANGED dropped
Drop ==
  /\ verdict = "drop"
  /\ dropped' = 1 - dropped
  /\ status' = "none" /\ verdict' = "" /\ iteration' = 0 /\ phaseTicks' = 0 /\ open' = 0 /\ blocked' = 0 /\ proposals' = 0
  /\ UNCHANGED shipped

Next == Select \/ Scaffold \/ Build \/ Patrol \/ Triage \/ ToEvaluate \/ Evaluate \/ Iterate \/ Ship \/ Drop

Spec == Init /\ [][Next]_vars /\ WF_vars(Next)

\* 生きていること: どのアプリも、いつか必ず終わって次に移る (1 つのアプリで止まり続けない)
EveryAppFinishes == [](status # "none" => <>(status = "none"))
\* 提案があるあいだに tick を使うのは、仕分けだけ (提案を残したまま作り続けない)
TriageFirst == [][(proposals > 0 /\ phaseTicks' = phaseTicks + 1) => proposals' = 0]_vars
====
