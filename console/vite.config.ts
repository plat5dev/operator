import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"

const api = "http://127.0.0.1:5004"

export default defineConfig({
  plugins: [react()],
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
      "/api": api,
    },
  },
  preview: {
    host: "127.0.0.1",
    port: 5173,
  },
})
