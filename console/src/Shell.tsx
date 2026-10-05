import { DownOutlined, MoonOutlined, SunOutlined, UserOutlined } from "@ant-design/icons"
import { Button, Dropdown, Empty, Layout } from "antd"
import { Link, Outlet, useLocation, useNavigate } from "react-router-dom"
import { readModules } from "./api"
import { useSession } from "./session"
import { useTheme } from "./theme"

const services = readModules()

export function ShellLayout() {
  const session = useSession()
  const { theme, toggleTheme } = useTheme()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const current = services.find((item) => pathname === item.basePath || pathname.startsWith(`${item.basePath}/`))

  async function logout() {
    await fetch("/logout", { method: "POST", credentials: "include" })
    navigate("/login", { replace: true })
  }

  const serviceLabel = services.length === 0 ? "No services" : (current?.title ?? "Services")

  return (
    <Layout className="shell-frame">
      <Layout.Header className="shell-header">
        <div className="shell-header-start">
          <Link to="/" className="shell-brand">
            Operator
          </Link>
          <Dropdown
            disabled={services.length === 0}
            menu={{
              selectedKeys: current ? [current.id] : [],
              items: services.map((service) => ({ key: service.id, label: service.title })),
              onClick: ({ key }) => {
                const service = services.find((item) => item.id === key)
                if (service) navigate(service.basePath)
              },
            }}
          >
            <Button type="text" className="shell-services" disabled={services.length === 0} icon={<DownOutlined />} iconPosition="end">
              <span className="shell-services-label">{serviceLabel}</span>
            </Button>
          </Dropdown>
        </div>
        <div className="shell-header-end">
          <Button
            type="text"
            aria-label={theme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
            icon={theme === "dark" ? <SunOutlined /> : <MoonOutlined />}
            onClick={toggleTheme}
          />
          <Dropdown
            menu={{
              items: [
                { key: "account", label: "Account settings" },
                { key: "logout", label: "Log out" },
              ],
              onClick: ({ key }) => {
                if (key === "account") navigate("/account")
                if (key === "logout") void logout()
              },
            }}
          >
            <Button type="text" icon={<UserOutlined />}>
              <span className="shell-account">{session.email}</span>
            </Button>
          </Dropdown>
        </div>
      </Layout.Header>
      <Layout.Content className="shell-content">
        <Outlet />
      </Layout.Content>
    </Layout>
  )
}

export function Well({ empty }: { empty: boolean }) {
  return <Empty description={empty ? "No services." : "Not found."} />
}
