// privacy-scan.mjs の自己検査: node --test scripts/privacy-scan.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, writeFileSync, readFileSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { check, hash } from "./privacy-scan.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const script = join(here, "privacy-scan.mjs");
// テストの中にも本物の値は書かない。DENY に入っている語の代わりに、同じ形の作り物で確かめる
const fake = "taro" + "yamada";

test("メールアドレス", () => {
  assert.deepEqual(check("連絡は someone@gmail.com まで"), ["メールアドレス"]);
  assert.deepEqual(check("Co-Authored-By: Claude <noreply@anthropic.com>"), []);
  assert.deepEqual(check("1+x@users.noreply.github.com git@github.com noreply@github.com a@example.com b@foo.test"), []);
  assert.deepEqual(check('"react@18.3.1" @types/node eslint@9.1.0'), []);
});

test("家のパス", () => {
  for (const s of [
    "/home/alice/src",
    "/Users/alice/x",
    "C:\\Users\\alice\\x",
    "/mnt/c/Users/alice/AppData",
  ]) {
    assert.deepEqual(check(s), ["家のパス"], s);
  }
  for (const s of ["/home/runner/work", "/home/user/x", "/home/$USER/x", "/Users/<you>/x", "/home/"]) {
    assert.deepEqual(check(s), [], s);
  }
});

test("名前 (DENY の hash と比べる)", () => {
  const src = readFileSync(script, "utf8");
  assert.ok(!src.includes(fake));
  assert.equal(hash("abc"), "ba7816bf8f01cfea");
  assert.deepEqual(check(`by ${fake}`), []);
  const deny = new Set([hash(fake)]);
  assert.deepEqual(check(`by ${fake.toUpperCase()}.x`, deny), ["名前"]);
  assert.deepEqual(check(`by ${fake}s`, deny), [], "語の一部は拾わない");
  assert.ok([...src.matchAll(/"([0-9a-f]{16})"/g)].length >= 1, "DENY は空でない");
});

function repo() {
  const d = mkdtempSync(join(tmpdir(), "privacy-"));
  const git = (...a) => {
    const r = spawnSync("git", a, { cwd: d, encoding: "utf8" });
    assert.equal(r.status, 0, r.stderr);
    return r.stdout;
  };
  git("init", "-q", "-b", "main");
  git("config", "user.name", "Rin0107");
  git("config", "user.email", "1+Rin0107@users.noreply.github.com");
  git("config", "commit.gpgsign", "false");
  return { d, git };
}

function run(d, ...args) {
  return spawnSync("node", [script, ...args], { cwd: d, encoding: "utf8" });
}

test("tree・range・all", () => {
  const { d, git } = repo();
  writeFileSync(join(d, "a.txt"), "hello\n");
  git("add", ".");
  git("commit", "-qm", "first");
  assert.equal(run(d, "tree").status, 0);
  assert.equal(run(d, "all").status, 0);
  const base = git("rev-parse", "HEAD").trim();

  writeFileSync(join(d, "b.txt"), "x\nmail: someone@gmail.com\n");
  git("add", ".");
  git("commit", "-qm", "second");
  const r = run(d, "range", `${base}..HEAD`);
  assert.equal(r.status, 1);
  assert.match(r.stderr, /b\.txt:2: メールアドレス/);
  assert.ok(!r.stderr.includes("someone"), "値を出さない");

  // 消しても履歴に残る: tree は通り、all は止まる
  git("rm", "-q", "b.txt");
  git("commit", "-qm", "third");
  assert.equal(run(d, "tree").status, 0);
  assert.equal(run(d, "all").status, 1);

  // 作者のメールアドレス
  const { d: d2, git: git2 } = repo();
  writeFileSync(join(d2, "a.txt"), "hi\n");
  git2("add", ".");
  git2("-c", "user.email=someone@gmail.com", "commit", "-qm", "x");
  const r2 = run(d2, "range", "0".repeat(40) + "..HEAD");
  assert.equal(r2.status, 1);
  assert.match(r2.stderr, /作者:1: メールアドレス/);

  assert.equal(run(d, "nope").status, 2);
});

test("雛形の写しは正本と同じ", () => {
  const root = join(here, "..");
  const src = readFileSync(script, "utf8");
  for (const t of readdirSync(join(root, "templates"))) {
    const p = join(root, "templates", t, "scripts", "privacy-scan.mjs");
    let copy;
    try {
      copy = readFileSync(p, "utf8");
    } catch {
      continue;
    }
    assert.equal(copy, src, p);
  }
});
