import { reactRouter } from "@react-router/dev/vite";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";
import { defineConfig, loadEnv } from "vite";

const workspaceRoot = fileURLToPath(new URL("../..", import.meta.url));

export default defineConfig(({ mode }) => {
  const environment = loadEnv(mode, workspaceRoot, "VITE_");
  const bffUrl = new URL(environment.VITE_BFF_URL || "http://localhost:3000");
  if (!/^https?:$/.test(bffUrl.protocol) || bffUrl.username || bffUrl.password || bffUrl.search || bffUrl.hash) {
    throw new Error("VITE_BFF_URL must be an HTTP(S) origin");
  }
  return {
    envDir: workspaceRoot,
    resolve: { tsconfigPaths: true },
    plugins: [tailwindcss(), reactRouter()],
    server: {
      proxy: {
        "/api": {
          target: bffUrl.origin,
          changeOrigin: true,
        },
      },
    },
  };
});
