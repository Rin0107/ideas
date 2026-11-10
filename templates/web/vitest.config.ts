// テストの設定と基準。守りの部分 (wakuwaku guard で書き換え禁止)。
// カバレッジは src/ の全部で 100% (行・分岐・関数・文)。下回れば npm test が落ちる。
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  test: {
    include: ["tests/unit/**/*.test.{ts,tsx}"],
    environment: "node",
    testTimeout: 60_000,
    coverage: {
      provider: "v8",
      include: ["src/**/*.{ts,tsx}"],
      exclude: ["src/**/*.d.ts"],
      reporter: ["text", "json-summary"],
      thresholds: { lines: 100, branches: 100, functions: 100, statements: 100 },
    },
  },
});
