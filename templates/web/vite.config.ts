/// <reference types="vitest/config" />
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  build: { sourcemap: false, assetsInlineLimit: 0 },
  test: { include: ["tests/unit/**/*.test.ts"], environment: "node" },
});
