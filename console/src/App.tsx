import { Navigate, Outlet, Route, Routes } from "react-router-dom"
import { AccountPage } from "./AccountPage"
import { readModules } from "./api"
import { LoginPage } from "./LoginPage"
import { ModuleHost } from "./module"
import { RequireSession } from "./session"
import { ShellHeader, Well } from "./Shell"

const modules = readModules()

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<AuthedShell />}>
        <Route path="/account" element={<AccountPage />} />
        {modules.map((mod) => (
          <Route key={mod.id} path={mountPath(mod.basePath)} element={<ModuleHost entry={mod.entry} />} />
        ))}
        {modules[0] ? <Route path="/" element={<Navigate to={modules[0].basePath} replace />} /> : null}
        <Route path="*" element={<Well empty={modules.length === 0} />} />
      </Route>
    </Routes>
  )
}

function mountPath(basePath: string): string {
  return `${basePath.replace(/^\//, "").replace(/\/$/, "")}/*`
}

function AuthedShell() {
  return (
    <RequireSession>
      <ShellHeader />
      <Outlet />
    </RequireSession>
  )
}
