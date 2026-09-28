import { createRequire } from "node:module"
import { mkdir, readFile, rm, writeFile } from "node:fs/promises"
import path from "node:path"
import { build } from "vite"

const prod = !process.argv.includes("--dev")
const require = createRequire(import.meta.url)
const nodeEnv = prod ? "production" : "development"
process.env.NODE_ENV = nodeEnv

const outDir = "public/vendor"
const genDir = path.join(outDir, ".entries")

function keys(id) {
  const mod = require(id)
  const api = mod && typeof mod === "object" && mod.default ? mod.default : mod
  return Object.keys(api).filter((key) => /^[A-Za-z_$][\w$]*$/.test(key))
}

function reexport(specifier, names, withDefault) {
  const lines = [`import * as ns from ${JSON.stringify(specifier)}`, "const api = ns.default ?? ns"]
  if (withDefault) lines.push("export default api")
  for (const name of names) lines.push(`export const ${name} = api.${name}`)
  return `${lines.join("\n")}\n`
}

// Rolldown leaves CJS require() of an external peer as a Node helper. The browser
// needs a static import of the same specifier the import map provides.
async function linkPeers(file) {
  let source = await readFile(file, "utf8")
  const ids = [...new Set([...source.matchAll(/__require\("([^"]+)"\)/g)].map((match) => match[1]))]
  if (ids.length === 0) return
  const imports = ids.map((id) => {
    const local = `__peer_${id.replace(/[^A-Za-z0-9]/g, "_")}`
    source = source.replaceAll(`__require("${id}")`, local)
    return `import ${local} from ${JSON.stringify(id)};`
  })
  if (source.includes("__require(")) throw new Error(`${file} still has a dynamic require`)
  await writeFile(file, `${imports.join("\n")}\n${source}`)
}

function externalPlugin(ids) {
  const set = new Set(ids)
  return {
    name: "external-peers",
    enforce: "pre",
    resolveId(id) {
      if (set.has(id)) return { id, external: true }
    },
  }
}

await rm(outDir, { recursive: true, force: true })
await mkdir(genDir, { recursive: true })

async function entryFile(name, source) {
  const file = path.join(genDir, name)
  await writeFile(file, source)
  return file
}

async function emit(fileName, entry, external) {
  await build({
    configFile: false,
    mode: nodeEnv,
    publicDir: false,
    logLevel: "warn",
    define: { "process.env.NODE_ENV": JSON.stringify(nodeEnv) },
    plugins: external.length ? [externalPlugin(external)] : [],
    build: {
      outDir,
      emptyOutDir: false,
      lib: {
        entry,
        formats: ["es"],
        fileName: () => fileName,
      },
      rollupOptions: { external },
      minify: false,
    },
  })
  await linkPeers(path.join(outDir, fileName))
}

await emit("react.js", await entryFile("react.js", reexport("react", keys("react"), true)), [])
await emit(
  "react-dom.js",
  await entryFile("react-dom.js", reexport("react-dom", keys("react-dom"), true)),
  ["react"],
)
await emit(
  "react-dom-client.js",
  await entryFile("react-dom-client.js", reexport("react-dom/client", keys("react-dom/client"), true)),
  ["react", "react-dom"],
)
await emit(
  "react-jsx-runtime.js",
  await entryFile("react-jsx-runtime.js", reexport("react/jsx-runtime", keys("react/jsx-runtime"), false)),
  ["react"],
)
await emit(
  "react-jsx-dev-runtime.js",
  await entryFile("react-jsx-dev-runtime.js", reexport("react/jsx-dev-runtime", keys("react/jsx-dev-runtime"), false)),
  ["react"],
)

const routerRoot = path.dirname(require.resolve("react-router/package.json"))
const variant = prod ? "production" : "development"
const routerEntry = [
  `export * from ${JSON.stringify(path.join(routerRoot, "dist", variant, "index.mjs"))}`,
  `export { HydratedRouter, RouterProvider } from ${JSON.stringify(path.join(routerRoot, "dist", variant, "dom-export.mjs"))}`,
  "",
].join("\n")
await emit(
  "react-router-dom.js",
  await entryFile("react-router-dom.js", routerEntry),
  ["react", "react-dom"],
)

await rm(genDir, { recursive: true, force: true })

async function text(name) {
  return readFile(path.join(outDir, name), "utf8")
}

function mustInclude(file, source, names) {
  for (const name of names) {
    if (!source.includes(name)) throw new Error(`${file} missing ${name}`)
  }
  if (source.includes("process.env.NODE_ENV")) throw new Error(`${file} still references process.env.NODE_ENV`)
}

const react = await text("react.js")
mustInclude("react.js", react, ["useState", "createElement"])
if (react.includes('from "react"') || react.includes("from 'react'")) {
  throw new Error("react.js must bundle react")
}

const dom = await text("react-dom.js")
mustInclude("react-dom.js", dom, ["createPortal", 'from "react"'])

const client = await text("react-dom-client.js")
mustInclude("react-dom-client.js", client, ["createRoot", 'from "react"', 'from "react-dom"'])

const jsx = await text("react-jsx-runtime.js")
mustInclude("react-jsx-runtime.js", jsx, ["jsx", "jsxs"])
if (jsx.includes('from "react/jsx-runtime"')) throw new Error("jsx runtime was not bundled")

const router = await text("react-router-dom.js")
mustInclude("react-router-dom.js", router, ["BrowserRouter", "useParams", 'from "react"'])
