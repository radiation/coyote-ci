import js from "@eslint/js";
import {
  createConfig,
  recommended as boundariesRecommended,
} from "eslint-plugin-boundaries/config";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";
import { defineConfig, globalIgnores } from "eslint/config";

const boundariesConfig = createConfig({
  files: ["src/**/*.{ts,tsx}"],
  ignores: ["src/**/*.test.{ts,tsx}", "src/test/**/*"],
  settings: {
    ...boundariesRecommended.settings,
    "boundaries/elements": [
      { type: "route", pattern: "src/routes/*", mode: "file" },
      { type: "page", pattern: "src/pages/*", mode: "file" },
      { type: "component", pattern: "src/components/*", mode: "file" },
      { type: "api", pattern: "src/api/*", mode: "file" },
      { type: "query", pattern: "src/queries/*", mode: "file" },
      { type: "shared", pattern: "src/utils/*", mode: "file" },
      { type: "shared", pattern: "src/types/*", mode: "file" },
      { type: "test-support", pattern: "src/test/*", mode: "file" },
    ],
  },
  rules: {
    ...boundariesRecommended.rules,
    "boundaries/dependencies": [
      "error",
      {
        default: "allow",
        rules: [
          {
            from: { type: "api" },
            disallow: [{ to: { type: ["page", "component", "route"] } }],
            message:
              "API modules may depend on shared contracts and request helpers, not pages, components, or routes.",
          },
          {
            from: { type: "query" },
            disallow: [{ to: { type: ["page", "component", "route"] } }],
            message:
              "Query helpers may depend on API and shared modules, not pages, components, or routes.",
          },
          {
            from: { type: "component" },
            disallow: [{ to: { type: ["page", "route"] } }],
            message:
              "Shared components should not import pages or route composition.",
          },
          {
            from: { type: "shared" },
            disallow: [{ to: { type: ["page", "component", "route"] } }],
            message:
              "Shared utilities and types must stay below page, component, and route layers.",
          },
          {
            from: {
              type: ["route", "page", "component", "api", "query", "shared"],
            },
            disallow: [{ to: { type: "test-support" } }],
            message: "Production modules must not import test helper modules.",
          },
        ],
      },
    ],
  },
});

export default defineConfig([
  globalIgnores(["coverage", "dist"]),
  {
    files: ["**/*.{ts,tsx}"],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    settings: {
      "import/resolver": {
        node: {
          extensions: [".js", ".jsx", ".ts", ".tsx"],
        },
      },
    },
  },
  boundariesConfig,
]);
