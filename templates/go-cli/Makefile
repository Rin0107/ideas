# テストの大きさ (Google の S / M / L、docs/TESTING.md):
#   S: 1 プロセス・I/O なし (単体・境界値・全件ループ・PBT・fuzz のシード)      make test
#   M: 手元で完結する複数プロセス (実行ファイルを作って動かす)                   make test-m
#   L: 本物の外部 (リリースした実行ファイル)                                  release.yml
# 速さ: CI のジョブはどれも 5 分で打ち切る (超えたら落ちる = merge できない)
.PHONY: verify lint test test-m cover fuzz mutate formal guards build
export CGO_ENABLED ?= 0

# 手元の機械を守る (2026-11-10 にメモリが尽きて PC ごと落ちた。docs/TESTING.md の「手元の機械を守る」):
#   並列の数 (JOBS) とメモリの上限 (MEM) を決めて動かす。手元では scripts/limit.sh が cgroup で強制する
#   (超えたらそのコマンドだけが止まる)。CI は使い捨ての 4 コアなので並列 4・上限なし
JOBS ?= $(if $(CI),4,2)
MEM ?= 2G
export GOMAXPROCS := $(JOBS)
export GOMEMLIMIT ?= 1GiB
LIMIT = sh scripts/limit.sh $(MEM) $(JOBS)

verify: lint cover guards build

lint:
	@test -z "$$(gofmt -l .)" || (echo "gofmt が必要:"; gofmt -l .; exit 1)
	$(LIMIT) go vet ./...
	$(LIMIT) go vet -tags medium ./...
	@if command -v golangci-lint >/dev/null; then $(LIMIT) golangci-lint run --concurrency $(JOBS); else echo "skip: golangci-lint (CI で動く)"; fi

test:
	$(LIMIT) go test -p $(JOBS) -count=1 ./...

test-m:
	$(LIMIT) go test -p $(JOBS) -count=1 -tags medium -run '^TestM_' ./...

# カバレッジは S と M を合わせて 100% (main も含む)。1 文でも通らない所があれば落ちる
cover:
	rm -rf .cover && mkdir -p .cover/s .cover/m
	$(LIMIT) go test -p $(JOBS) -count=1 -cover -coverpkg=./... ./... -args -test.gocoverdir=$(CURDIR)/.cover/s
	COVER_M=$(CURDIR)/.cover/m $(LIMIT) go test -p $(JOBS) -count=1 -tags medium -run '^TestM_' ./...
	go tool covdata textfmt -i=.cover/s,.cover/m -o .cover/all.out
	sh scripts/cover-check.sh .cover/all.out

# fuzzing: 新しい入力を探す。全部の Fuzz で合わせて FUZZ_SECONDS 秒 (並列と上限は scripts/fuzz.sh が自分でかける)
FUZZ_SECONDS ?= 150
fuzz:
	FUZZ_PARALLEL=$(JOBS) sh scripts/fuzz.sh $(FUZZ_SECONDS)

# ミューテーション: コードを少し壊して、テストが気づくか。BASE からの変更だけ (CI が渡す)。BASE が空なら全部
# gremlins は既定で全コア (runtime.NumCPU) の数だけ go test を同時に動かすので、--workers で決める
MUTATE_MEM ?= 3G
BASE ?= origin/main
mutate:
	sh scripts/limit.sh $(MUTATE_MEM) $(JOBS) gremlins unleash --workers $(JOBS) --test-cpu 1 $(if $(BASE),--diff "$(BASE)") --threshold-efficacy 95 --threshold-mcover 100 --timeout-coefficient 3 --invert-logical --invert-loopctrl

# 仕様の検査: TLA+ (TLC) と Z3。道具は CI が版と sha256 を固定して入れる (並列と上限は scripts/formal.sh が自分でかける)
formal:
	sh scripts/formal.sh

guards:
	node scripts/go-guard.mjs
	$(LIMIT) go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

build:
	$(LIMIT) go build -trimpath -o /dev/null ./cmd/...
