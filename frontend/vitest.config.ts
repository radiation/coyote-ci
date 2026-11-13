import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import { applicationVersionDefine } from "./build-version";

export default defineConfig({
  plugins: [react()],
  define: {
    __COYOTE_VERSION__: applicationVersionDefine,
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
    coverage: {
      provider: "v8",
      reporter: ["text", "lcov"],
      reportsDirectory: "./coverage",
    },
  },
});
