// 依存の検査: 禁止パッケージ (解析・課金・LLM・BaaS) がない、バージョンが固定されている、lockfile がある。
import { existsSync, readFileSync } from "node:fs";
import { fail, wk } from "./lib.mjs";

const pkg = JSON.parse(readFileSync("package.json", "utf8"));
const problems = [];
const all = { ...pkg.dependencies, ...pkg.devDependencies };
for (const [name, ver] of Object.entries(all)) {
  if (wk.deny_packages.includes(name))
    problems.push(`${name} は使えない (重要な情報を外に出す・お金がかかる)`);
  if (!/^\d+\.\d+\.\d+(-[\w.]+)?$/.test(ver))
    problems.push(`${name}@${ver}: バージョンを固定する (npm install -E)`);
}
if (Object.keys(pkg.dependencies ?? {}).length > 8) problems.push("本番の依存が 8 個を超えた (減らす)");
if (!existsSync("package-lock.json")) problems.push("package-lock.json がない");
if (pkg.scripts?.postinstall || pkg.scripts?.preinstall) problems.push("install のスクリプトは使わない");
fail("依存 (deps-guard)", problems);
