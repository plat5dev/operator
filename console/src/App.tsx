import { Outlet, Route, Routes } from "react-router-dom"
import { AccountPage } from "./AccountPage"
import { LoginPage } from "./LoginPage"
import { RequireSession } from "./session"
import { ShellHeader, Well } from "./Shell"

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<AuthedShell />}>
        <Route path="/account" element={<AccountPage />} />
        <Route path="*" element={<Well />} />
      </Route>
    </Routes>
  )
}

function AuthedShell() {
  return (
    <RequireSession>
      <ShellHeader />
      <Outlet />
    </RequireSession>
  )
}
