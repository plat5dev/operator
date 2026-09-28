import { readdir, readFile, rm, writeFile } from "node:fs/promises"
import path from "node:path"
import { defineConfig, type Plugin } from "vite"
import react from "@vitejs/plugin-react"

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

function injectCss(): Plugin {
  return {
    name: "inject-css",
    apply: "build",
    async closeBundle() {
      const out = path.resolve("../dist/identity")
      const files = await readdir(out).catch(() => [])
      const cssFiles = files.filter((file) => file.endsWith(".css"))
      const jsPath = path.join(out, "entry.js")
      const js = await readFile(jsPath, "utf8")
      // The bundle runs in the browser with no `process` global. Vite does not
      // replace process.env in lib builds, so fail loudly instead of shipping
      // an entry.js that throws "process is not defined" on import.
      if (js.includes("process.env.NODE_ENV")) {
        throw new Error("entry.js still references process.env.NODE_ENV (missing define)")
      }
      if (js.startsWith("const style=")) return
      if (cssFiles.length === 0) return
      let css = ""
      for (const file of cssFiles) css += await readFile(path.join(out, file), "utf8")
      const payload = JSON.stringify(css)
      await writeFile(jsPath, `const style=document.createElement("style");style.textContent=${payload};document.head.appendChild(style);\n${js}`)
      await Promise.all(cssFiles.map((file) => rm(path.join(out, file))))
    },
  }
}

export default defineConfig({
  plugins: [externalPeers(), react(), injectCss()],
  define: { "process.env.NODE_ENV": JSON.stringify("production") },
  build: {
    outDir: "../dist/identity",
    emptyOutDir: true,
    lib: {
      entry: "src/index.tsx",
      formats: ["es"],
      fileName: () => "entry.js",
    },
    rollupOptions: { external: peers },
  },
})
