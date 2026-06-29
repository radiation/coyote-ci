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
  settings: {
    ...boundariesRecommended.settings,
    "boundaries/elements": [
      { type: "test-module", pattern: "src/**/*.test.{ts,tsx}", mode: "file" },
      { type: "test-support", pattern: "src/test/**/*.{ts,tsx}", mode: "file" },
      { type: "style", pattern: "src/**/*.css", mode: "file" },
      { type: "app", pattern: "src/App.tsx", mode: "file" },
      { type: "app", pattern: "src/main.tsx", mode: "file" },
      { type: "auth", pattern: "src/auth.tsx", mode: "file" },
      { type: "auth", pattern: "src/auth-context.ts", mode: "file" },
      { type: "theme", pattern: "src/theme.tsx", mode: "file" },
      { type: "theme", pattern: "src/theme-context.ts", mode: "file" },
      { type: "theme", pattern: "src/theme-shared.ts", mode: "file" },
      { type: "route", pattern: "src/routes/**/*.{ts,tsx}", mode: "file" },
      {
        type: "page-support",
        pattern: "src/pages/*.helpers.{ts,tsx}",
        mode: "file",
      },
      {
        type: "page-support",
        pattern: "src/pages/*.sections.{ts,tsx}",
        mode: "file",
      },
      { type: "page-support", pattern: "src/pages/*Form.tsx", mode: "file" },
      { type: "page", pattern: "src/pages/**/*.{ts,tsx}", mode: "file" },
      {
        type: "component",
        pattern: "src/components/**/*.{ts,tsx}",
        mode: "file",
      },
      { type: "api", pattern: "src/api/**/*.{ts,tsx}", mode: "file" },
      { type: "query", pattern: "src/queries/**/*.{ts,tsx}", mode: "file" },
      { type: "type", pattern: "src/types/**/*.{ts,tsx}", mode: "file" },
      { type: "utility", pattern: "src/utils/**/*.{ts,tsx}", mode: "file" },
    ],
  },
  rules: {
    ...boundariesRecommended.rules,
    "boundaries/no-unknown-files": "error",
    "boundaries/no-unknown": "error",
    "boundaries/dependencies": [
      "error",
      {
        default: "disallow",
        checkUnknownLocals: true,
        rules: [
          {
            from: { type: "app" },
            allow: [
              {
                to: {
                  type: [
                    "app",
                    "auth",
                    "theme",
                    "route",
                    "api",
                    "type",
                    "utility",
                    "style",
                  ],
                },
              },
            ],
            message:
              "App composition should depend only on providers, routes, API entrypoints, and shared support modules.",
          },
          {
            from: { type: "auth" },
            allow: [{ to: { type: ["auth", "api", "type", "utility"] } }],
            message:
              "Auth providers and context should depend on API, auth-local modules, and shared contracts only.",
          },
          {
            from: { type: "theme" },
            allow: [{ to: { type: ["theme", "type", "utility"] } }],
            message:
              "Theme modules should stay inside theme support and shared helpers.",
          },
          {
            from: { type: "route" },
            allow: [
              {
                to: {
                  type: [
                    "page",
                    "component",
                    "auth",
                    "theme",
                    "type",
                    "utility",
                  ],
                },
              },
            ],
            message:
              "Routes may compose pages, auth, and shared helpers, but should not become a data-access or business-logic layer.",
          },
          {
            from: { type: "page" },
            allow: [
              {
                to: {
                  type: [
                    "component",
                    "page-support",
                    "query",
                    "api",
                    "auth",
                    "theme",
                    "type",
                    "utility",
                  ],
                },
              },
            ],
            message:
              "Pages may orchestrate UI, queries, public API exports, auth, and shared helpers, but should not depend on routes or other pages.",
          },
          {
            from: { type: "page-support" },
            allow: [
              {
                to: {
                  type: [
                    "component",
                    "page-support",
                    "query",
                    "api",
                    "auth",
                    "theme",
                    "type",
                    "utility",
                  ],
                },
              },
            ],
            message:
              "Page-support modules should stay below routes and page containers.",
          },
          {
            from: { type: "api" },
            allow: [{ to: { type: ["api", "type", "utility"] } }],
            message:
              "API modules may depend on shared contracts and request helpers, not pages, components, or routes.",
          },
          {
            from: { type: "query" },
            allow: [{ to: { type: ["api", "type", "utility"] } }],
            message:
              "Query helpers may depend on API and shared modules, not pages, routes, or feature UI.",
          },
          {
            from: { type: "component" },
            allow: [
              {
                to: {
                  type: [
                    "component",
                    "api",
                    "query",
                    "auth",
                    "theme",
                    "type",
                    "utility",
                  ],
                },
              },
            ],
            message:
              "Shared components should not import pages, routes, or app composition.",
          },
          {
            from: { type: "type" },
            allow: [{ to: { type: ["type"] } }],
            message:
              "Shared type modules must remain independent from UI and composition layers.",
          },
          {
            from: { type: "utility" },
            allow: [{ to: { type: ["type", "utility"] } }],
            message:
              "Shared utilities must stay below UI and composition layers.",
          },
          {
            from: { type: "test-module" },
            allow: [
              {
                to: {
                  type: [
                    "app",
                    "auth",
                    "theme",
                    "route",
                    "page",
                    "page-support",
                    "component",
                    "api",
                    "query",
                    "type",
                    "utility",
                    "test-support",
                  ],
                },
              },
            ],
            message:
              "Tests may import production modules and test support, but they should not create new production dependency directions.",
          },
          {
            from: { type: "test-support" },
            allow: [
              {
                to: {
                  type: ["auth", "theme", "type", "utility", "test-support"],
                },
              },
            ],
            message:
              "Test-support modules should depend on shared production contracts or other test support only.",
          },
          {
            from: {
              type: [
                "app",
                "auth",
                "theme",
                "route",
                "page",
                "page-support",
                "component",
                "api",
                "query",
                "type",
                "utility",
              ],
            },
            disallow: [{ to: { type: ["test-module", "test-support"] } }],
            message:
              "Production modules must not import test files or test-support helpers.",
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
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: [
      "src/**/*.test.{ts,tsx}",
      "src/test/**/*.{ts,tsx}",
      "src/api/**/*.{ts,tsx}",
    ],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["./api/*", "../api/*", "../../api/*", "../../../api/*"],
              message:
                "Import production API modules through the src/api public entrypoint instead of deep-importing private implementation files.",
            },
          ],
        },
      ],
    },
  },
  boundariesConfig,
]);
