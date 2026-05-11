import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { THEME_STORAGE_KEY } from "./src/theme-shared";

const backendTarget = process.env.VITE_API_BASE || "http://localhost:8080";

export default defineConfig({
  plugins: [
    {
      name: "inject-theme-storage-key",
      transformIndexHtml(html) {
        return html.replace(/__THEME_STORAGE_KEY__/g, THEME_STORAGE_KEY);
      },
    },
    react(),
  ],
  server: {
    port: 3000,
    proxy: {
      "/api": {
        target: backendTarget,
        changeOrigin: true,
      },
      "/auth": {
        target: backendTarget,
        changeOrigin: true,
      },
    },
  },
});
