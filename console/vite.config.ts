import { defineConfig, type Plugin } from "vite"
import react from "@vitejs/plugin-react"

const api = "http://127.0.0.1:5004"

const peers = [
  "react",
  "react-dom",
  "react-dom/client",
  "react/jsx-runtime",
  "react/jsx-dev-runtime",
  "react-router-dom",
]

function externalPeers(): Plugin {
  const set = new Set(peers)
  return {
    name: "external-peers",
    enforce: "pre",
    resolveId(id) {
      if (set.has(id)) return { id, external: true }
    },
  }
}

export default defineConfig({
  plugins: [externalPeers(), react()],
  optimizeDeps: { exclude: peers },
  build: {
    rollupOptions: { external: peers },
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    proxy: {
      "/login": {
        target: api,
        bypass(req) {
          if (req.method !== "POST") return req.url
        },
      },
      "/logout": api,
      "/session": api,
      "/account/password": api,
      "/organizations": api,
      "/users": api,
      "/members": api,
      "/modules": api,
    },
  },
  preview: {
    host: "127.0.0.1",
    port: 5173,
  },
})
