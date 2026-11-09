// Package guard は Claude Code の PreToolUse フックで、工場の外への影響・お金・秘密情報・守りの部分の書き換えを止める。
// このパッケージ自体も守りの部分 (factory.json の protected_paths)。エージェントは書き換えられない。
//
// guard は 1 枚目の壁。すり抜けられた場合の 2 枚目は wk sync / wk merge の「守りの部分の差分の検査」と CI。
package guard

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Context は判定に使う情報。
type Context struct {
	Owner     string          // Rin0107
	Factory   string          // Rin0107/ideas
	Owned     map[string]bool // portfolio にある repo (工場が作ったもの)
	Root      string          // 今の repo の最上位 (相対パスの解決に使う)
	Protected []string        // 守りの部分 (Root からの相対パス。末尾 / はディレクトリ)
	Writable  []string        // Edit/Write してよい場所 (絶対パス)。Root はいつも含む
}

type rule struct {
	re     *regexp.Regexp
	reason string
}

func r(expr, reason string) rule { return rule{regexp.MustCompile(expr), reason} }

const home = `(~|\$HOME|\$\{HOME\}|/home/[^/\s"']+|/root)`

// 無条件で止めるもの
var denyRules = []rule{
	// GitHub・git
	r(`\bgh\s+repo\s+delete\b`, "repo の削除は禁止"),
	r(`\bgh\s+repo\s+create\b`, "repo は wk scaffold で作る"),
	r(`\bgh\s+repo\s+edit\b[^|;&]*--visibility`, "公開範囲の変更は wk ship で行う"),
	r(`\bgh\s+repo\s+archive\b`, "archive は wk drop で行う"),
	r(`\bgh\s+pr\s+merge\b`, "merge は wk merge で行う (CI と reviewer を待つため)"),
	r(`\bgh\s+api\b[^|;&]*(-X|--method)\s*DELETE`, "API での削除は禁止"),
	r(`\bgh\s+api\s+graphql\b[^|;&]*\bmutation\b`, "GraphQL での書き込みは禁止"),
	r(`\bgh\s+(secret|variable)\b`, "secret・variable は触らない"),
	r(`\bgh\s+(auth|config|extension|alias)\b`, "gh の認証・設定は触らない"),
	r(`\bgit\s+push\b[^|;&]*(\s--force\b|\s-f\b|\s--force-with-lease\b|\s--mirror\b|\s--delete\b|\s\+\S|\s:\S)`, "force push・削除の push は禁止"),
	r(`\bgit\s+push\b[^|;&]*\s(origin\s+)?(HEAD:)?(refs/heads/)?(main|master)\b`, "main への push は wk merge / wk sync だけが行う"),
	r(`\bgit\s+(reset\s+--hard|clean\s+-[a-z]*f)`, "作業の破棄は禁止 (worktree を作り直す)"),
	r(`\bgit\s+remote\s+(add|set-url|rename|remove)\b`, "remote は変えない (外の場所に push しない)"),
	r(`\bgit\s+config\b[^|;&]*(--global|--system|credential|core\.hooksPath|url\.)`, "git の全体設定・認証の設定は変えない"),
	r(`\bgit\s+(filter-branch|filter-repo|replace)\b`, "履歴の書き換えは禁止"),
	r(`--no-verify\b`, "フックを飛ばさない"),
	// お金
	r(`\b(gh\s+api|wrangler|vercel|supabase|curl)\b[^|;&]*(?i:billing|subscription|payment)`, "支払い・契約に関わる操作は禁止"),
	r(`\b(wrangler|vercel|supabase)\b[^|;&]*(--plan|\bupgrade\b|\bpro\b)`, "有料プランへの操作は禁止"),
	r(`\bvercel\s+(teams?|domains\s+buy|domains\s+add)\b`, "Vercel のチーム・ドメインの操作は禁止"),
	r(`\bwrangler\s+(domains?|zones?|login|logout|whoami)\b`, "Cloudflare のドメイン・ログインの操作は禁止"),
	r(`\bwrangler\s+(deploy|publish|delete|versions|rollback|d1|kv|r2|queues)\b`, "Cloudflare への操作は wk deploy だけが行う"),
	r(`\bsupabase\b`, "Supabase は使わない"),
	// 秘密情報を読まない・出さない (鍵は wk が子プロセスの環境変数で渡すだけ)
	r(`\.config/wakuwaku|\.ssh/|\.aws/|\.config/gh/|hosts\.yml|\.claude\.json|\.credentials|\.netrc|\.git-credentials|credentials\.json|\.dev\.vars`, "秘密情報のファイルに触れない"),
	r(home+`/\.claude\b`, "Claude Code の設定・認証に触れない"),
	r(`(^|[|;&]\s*)(printenv|env|set|export\s+-p|declare\s+-p)\s*($|[|;&>])`, "環境変数を一覧しない"),
	r(`\$\{?\w*(TOKEN|SECRET|PASSWORD|API_KEY|PRIVATE_KEY|CREDENTIAL)\w*\}?`, "秘密情報の環境変数を参照しない"),
	r(`/proc/[^\s]*/environ`, "ほかのプロセスの環境変数を読まない"),
	r(`\bwrangler\s+secret\b`, "アプリに秘密情報を持たせない"),
	// 工場の守り (wk の差し替え・止める仕組みの解除)
	r(`\bgo\s+(install|build\b[^|;&]*-o)\b`, "wk の入れ直し・バイナリの置き換えは人だけが行う"),
	r(home+`/go/bin|\.local/state/wakuwaku|(\brm\b|\bmv\b|\bunlink\b)[^|;&]*\bSTOP\b`, "wk の実行ファイル・止める仕組みには触れない"),
	r(`\.claude/settings\.local\.json|disableAllHooks`, "フックを無効にしない"),
	r(home+`/(src/okuos|src/nix-config|src/it-is-me|\.bashrc|\.profile|\.zshrc|\.config)\b[^|;&]*(>|\btee\b|\brm\b|\bmv\b|\bcp\b|sed\s+-i)`, "工場の外のファイルを書き換えない"),
	r(`(>|\btee\b[^|;&]*)\s*`+home+`/(\.|src/okuos|src/nix-config|src/it-is-me|go/)`, "工場の外のファイルに書き込まない"),
	// その他
	r(`\b(curl|wget)\b[^|;&]*\|\s*(ba|z)?sh\b`, "ダウンロードしたスクリプトをそのまま実行しない"),
	r(`\brm\s+-[a-z]*r[a-z]*f?\s+(/|~|\$HOME|\.\.)(\s|/|$)`, "worktree の外を消さない"),
	r(`\b(sudo|doas|chmod\s+777|chown)\b`, "権限の変更は禁止"),
	r(`\bnpm\s+(publish|adduser|login|token|owner)\b|\bnpx\s+--yes\b`, "npm の公開・ログイン・確認なしの実行は禁止"),
	r(`\bnpm\s+install-scripts\s+approve\b|--allow-scripts|--unsafe-perm`, "install script の許可は人だけが行う"),
}

var (
	repoPos  = regexp.MustCompile(`\bgh\s+repo\s+(?:edit|rename|sync|set-default|view)\s+([\w.-]+/[\w.-]+)`)
	repoFlag = regexp.MustCompile(`(?:--repo|-R)[\s=]+([\w.-]+/[\w.-]+)`)
	apiRepo  = regexp.MustCompile(`\bgh\s+api\b[^|;&]*\brepos/([\w.-]+/[\w.-]+)`)
	ghCmd    = regexp.MustCompile(`\bgh\s+(issue|pr|label|release|run|workflow|api|repo)\b`)
	ghRead   = regexp.MustCompile(`\bgh\s+(issue|pr|release|run|repo)\s+(list|view|diff|checks|status)\b`)
	ghWrite  = regexp.MustCompile(`(-X|--method)\s*(POST|PUT|PATCH|DELETE)|\s-f\s|\s-F\s|--field|--raw-field|--input`)
	segSplit = regexp.MustCompile(`\s*(\|\|?|&&|;)\s*`)
)

// 守りの部分に触れてよいのは、読むだけのコマンド
var readOnly = map[string]bool{
	"cat": true, "less": true, "head": true, "tail": true, "grep": true, "rg": true, "wc": true, "ls": true,
	"diff": true, "jq": true, "stat": true, "file": true, "sha256sum": true, "wk": true, "test": true, "[": true,
}
var readOnlyGit = regexp.MustCompile(`^git\s+(-C\s+\S+\s+)?(diff|log|show|status|blame|ls-files|grep)\b`)

// Bash はコマンドを止めるべきなら理由を返す (止めないなら "")。
func Bash(cmd string, c Context) string {
	for _, rl := range denyRules {
		if rl.re.MatchString(cmd) {
			return rl.reason
		}
	}
	// gh で書き込む repo は、工場とポートフォリオの repo だけ (読むだけなら公開 OSS も可)
	if ghCmd.MatchString(cmd) {
		for _, m := range append(append(repoFlag.FindAllStringSubmatch(cmd, -1), apiRepo.FindAllStringSubmatch(cmd, -1)...), repoPos.FindAllStringSubmatch(cmd, -1)...) {
			repo := m[1]
			if repo == c.Factory || c.Owned[repo] || isReadOnlyGh(cmd) {
				continue
			}
			return fmt.Sprintf("%s は工場の repo ではない (書き込みは portfolio の repo だけ)", repo)
		}
	}
	// 守りの部分に触れるコマンドは、読むだけのものに限る (python・perl・node などでの書き換えも止める)
	for _, p := range c.Protected {
		if !strings.Contains(cmd, strings.TrimSuffix(p, "/")) {
			continue
		}
		for _, seg := range segSplit.Split(strings.TrimSpace(cmd), -1) {
			f := strings.Fields(seg)
			if len(f) == 0 {
				continue
			}
			if strings.ContainsAny(seg, ">") || !(readOnly[f[0]] || readOnlyGit.MatchString(seg)) {
				return p + " は守りの部分 (読むだけならよい。書き換えはユーザーだけ)"
			}
			if f[0] == "jq" && strings.Contains(seg, "-i") {
				return p + " は守りの部分"
			}
		}
	}
	return ""
}

func isReadOnlyGh(cmd string) bool {
	if regexp.MustCompile(`\bgh\s+api\b`).MatchString(cmd) {
		return !ghWrite.MatchString(cmd)
	}
	return ghRead.MatchString(cmd)
}

// Edit はファイルの書き換えを止めるべきなら理由を返す。
// 書いてよいのは Root (今の repo) と Writable の中だけ。その中でも守りの部分は書けない。
func Edit(path string, c Context) string {
	if path == "" {
		return ""
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(c.Root, abs)
	}
	abs = filepath.Clean(abs)
	if strings.Contains(abs, "/.claude/settings.local.json") {
		return "フックの設定は変えない"
	}
	rel, err := filepath.Rel(c.Root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		for _, w := range c.Writable {
			if r, err := filepath.Rel(w, abs); err == nil && !strings.HasPrefix(r, "..") {
				return ""
			}
		}
		return abs + " は工場の外 (書けるのは工場・アプリの作業場所・一時ディレクトリだけ)"
	}
	rel = filepath.ToSlash(rel)
	for _, p := range c.Protected {
		if rel == strings.TrimSuffix(p, "/") || strings.HasSuffix(p, "/") && strings.HasPrefix(rel, p) {
			return p + " は守りの部分 (ユーザーだけが変える)"
		}
	}
	return ""
}

// TouchesProtected は変更されたファイルの一覧のうち、守りの部分にあたるものを返す (wk sync / wk merge の 2 枚目の壁)。
func TouchesProtected(files, protected []string) []string {
	var hit []string
	for _, f := range files {
		f = filepath.ToSlash(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		for _, p := range protected {
			if f == strings.TrimSuffix(p, "/") || strings.HasSuffix(p, "/") && strings.HasPrefix(f, p) {
				hit = append(hit, f)
				break
			}
		}
	}
	return hit
}
