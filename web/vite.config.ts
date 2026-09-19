import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const backend = {
  target: "http://127.0.0.1:8000",
  changeOrigin: true,
};

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/analysis": backend,
      "/healthz": backend,
      "/readyz": backend,
      "/version": backend,
    },
  },
});
