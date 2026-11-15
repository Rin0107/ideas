#!/bin/sh
# Claude Code の statusline (~/.claude/settings.json の statusLine から呼ぶ)。
# 受け取った rate_limits (5 時間・週の利用上限) を wk が読むファイルに書き留め、そのまま Orca の statusline に渡す。
# 鍵を持たずに利用上限を知るため (docs/DESIGN-v3.md §9)。書き留められなくても statusline は止めない
out="${WK_LIMITS_FILE:-$HOME/.local/state/wakuwaku/rate-limits.json}"
p=$(cat)
case "$p" in
  *'"rate_limits"'*)
    mkdir -p "$(dirname "$out")" 2>/dev/null
    tmp="$out.$$"
    if printf '%s' "$p" | jq -ce 'select(.rate_limits.five_hour != null and .rate_limits.seven_day != null) | {at: (now | floor), rate_limits}' >"$tmp" 2>/dev/null; then
      mv -f "$tmp" "$out"
    else
      rm -f "$tmp"
    fi
    ;;
esac
orca="$HOME/.orca/agent-hooks/claude-statusline.sh"
if [ -f "$orca" ] && [ -r "$orca" ] && [ -x "$orca" ]; then
  printf '%s' "$p" | /bin/sh "$orca"
fi
exit 0
