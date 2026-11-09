// Package guard は Claude Code の PreToolUse フックで、工場の外への影響・お金・守りの部分の書き換えを止める。
// このパッケージ自体も守りの部分 (factory.json の protected_paths)。エージェントは書き換えられない。
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
}

type rule struct {
	re     *regexp.Regexp
	reason string
}

func r(expr, reason string) rule { return rule{regexp.MustCompile(expr), reason} }

// 無条件で止めるもの
var denyRules = []rule{
	r(`\bgh\s+repo\s+delete\b`, "repo の削除は禁止"),
	r(`\bgh\s+repo\s+create\b`, "repo は wk scaffold で作る"),
	r(`\bgh\s+repo\s+edit\b[^|;&]*--visibility`, "公開範囲の変更は wk ship で行う"),
	r(`\bgh\s+repo\s+archive\b`, "archive は wk drop で行う"),
	r(`\bgh\s+pr\s+merge\b`, "merge は wk merge で行う (CI と reviewer を待つため)"),
	r(`\bgh\s+api\b[^|;&]*(-X|--method)\s*DELETE`, "API での削除は禁止"),
	r(`\bgh\s+(secret|variable)\s+(delete|remove)\b`, "secret の削除は禁止"),
	r(`\bgit\s+push\b[^|;&]*(\s--force\b|\s-f\b|\s--force-with-lease\b|\s\+\S)`, "force push は禁止"),
	r(`\bgit\s+push\b[^|;&]*\s(origin\s+)?(HEAD:)?(main|master)\b`, "main への push は wk merge / wk sync だけが行う"),
	r(`\bgit\s+(reset\s+--hard|clean\s+-[a-z]*f)`, "作業の破棄は禁止 (worktree を作り直す)"),
	r(`\b(gh\s+api|wrangler|vercel|supabase|curl)\b[^|;&]*(?i:billing|subscription|payment)`, "支払い・契約に関わる操作は禁止"),
	r(`\b(wrangler|vercel|supabase)\b[^|;&]*(--plan|\bupgrade\b|\bpro\b)`, "有料プランへの操作は禁止"),
	r(`\bvercel\s+(teams?|domains\s+buy|domains\s+add)\b`, "Vercel のチーム・ドメインの操作は禁止"),
	r(`\bwrangler\s+(domains?|zones?)\b`, "Cloudflare のドメイン操作は禁止"),
	r(`\bsupabase\s+(orgs|projects)\s+create\b`, "Supabase のプロジェクトは共有の 1 つだけを使う"),
	r(`\b(curl|wget)\b[^|;&]*\|\s*(ba|z)?sh\b`, "ダウンロードしたスクリプトをそのまま実行しない"),
	r(`\brm\s+-[a-z]*r[a-z]*f?\s+(/|~|\$HOME|\.\.)(\s|/|$)`, "worktree の外を消さない"),
	r(`\b(sudo|chmod\s+777)\b`, "権限の変更は禁止"),
	// 秘密情報を読まない・出さない (鍵は wk と wrangler が環境変数で受け取るだけ)
	r(`\.config/wakuwaku|\.ssh/|\.aws/|\.config/gh/|hosts\.yml|\.claude\.json|\.netrc|\.git-credentials|credentials\.json|\.dev\.vars`, "秘密情報のファイルに触れない"),
	r(`\bgh\s+auth\s+(token|status\s+-t|status\s+--show-token)`, "トークンを表示しない"),
	r(`(^|[|;&]\s*)(printenv|env|set|export\s+-p)\s*($|[|;&>])`, "環境変数を一覧しない"),
	r(`\$\{?\w*(TOKEN|SECRET|PASSWORD|API_KEY|PRIVATE_KEY)\w*\}?`, "秘密情報の環境変数を参照しない"),
	r(`\bwrangler\s+secret\b`, "アプリに秘密情報を持たせない"),
}

var (
	repoFlag = regexp.MustCompile(`(?:--repo|-R)[\s=]+([\w.-]+/[\w.-]+)`)
	apiRepo  = regexp.MustCompile(`\bgh\s+api\b[^|;&]*\brepos/([\w.-]+/[\w.-]+)`)
	ghCmd    = regexp.MustCompile(`\bgh\s+(issue|pr|label|release|run|workflow|secret|variable|api)\b`)
	writeOps = regexp.MustCompile(`(>|>>|\bsed\s+-i|\brm\b|\bmv\b|\bcp\b|\btee\b|\btruncate\b|\bchmod\b|\bln\b|\bgit\s+(rm|mv|restore|checkout))`)
	ghRead   = regexp.MustCompile(`\bgh\s+(issue|pr|release|run)\s+(list|view|diff|checks|status)\b|\bgh\s+api\b[^|;&]*$`)
)

// Bash はコマンドを止めるべきなら理由を返す (止めないなら "")。
func Bash(cmd string, c Context) string {
	for _, rl := range denyRules {
		if rl.re.MatchString(cmd) {
			return rl.reason
		}
	}
	// gh で触る repo は、工場とポートフォリオの repo だけ (読むだけなら公開 OSS も可)
	if ghCmd.MatchString(cmd) {
		for _, m := range append(repoFlag.FindAllStringSubmatch(cmd, -1), apiRepo.FindAllStringSubmatch(cmd, -1)...) {
			repo := m[1]
			if repo == c.Factory || c.Owned[repo] {
				continue
			}
			if isReadOnlyGh(cmd) {
				continue
			}
			return fmt.Sprintf("%s は工場の repo ではない (書き込みは portfolio の repo だけ)", repo)
		}
	}
	// 守りの部分をシェルで書き換えない
	if writeOps.MatchString(cmd) {
		for _, p := range c.Protected {
			if strings.Contains(cmd, strings.TrimSuffix(p, "/")) {
				return p + " は守りの部分 (ユーザーだけが変える)"
			}
		}
	}
	return ""
}

func isReadOnlyGh(cmd string) bool {
	if strings.Contains(cmd, "gh api") {
		return !regexp.MustCompile(`(-X|--method)\s*(POST|PUT|PATCH|DELETE)|\s-f\s|\s-F\s|--field|--raw-field|--input`).MatchString(cmd)
	}
	return ghRead.MatchString(cmd)
}

// Edit はファイルの書き換えを止めるべきなら理由を返す。
func Edit(path string, c Context) string {
	if path == "" {
		return ""
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(c.Root, abs)
	}
	rel, err := filepath.Rel(c.Root, filepath.Clean(abs))
	if err != nil || strings.HasPrefix(rel, "..") {
		return "" // repo の外は Claude Code 自身の権限に任せる
	}
	rel = filepath.ToSlash(rel)
	for _, p := range c.Protected {
		if rel == strings.TrimSuffix(p, "/") || strings.HasSuffix(p, "/") && strings.HasPrefix(rel, p) {
			return p + " は守りの部分 (ユーザーだけが変える)"
		}
	}
	return ""
}
