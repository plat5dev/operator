import { useNavigate } from "react-router-dom"
import Box from "@cloudscape-design/components/box"
import ButtonDropdown from "@cloudscape-design/components/button-dropdown"
import Link from "@cloudscape-design/components/link"
import TopNavigation from "@cloudscape-design/components/top-navigation"
import { readServices } from "./api"
import { useSession } from "./session"

const services = readServices()

export function ShellHeader() {
  const session = useSession()
  const navigate = useNavigate()
  const items = services.length
    ? services.map((service) => ({ id: service.id, text: service.title }))
    : [{ id: "none", text: "No services", disabled: true }]

  async function logout() {
    await fetch("/logout", { method: "POST", credentials: "include" })
    navigate("/login", { replace: true })
  }

  return (
    <TopNavigation visualContext="top-navigation">
      <div className="shell-bar">
        <div className="shell-bar-start">
          <ButtonDropdown
            items={items}
            ariaLabel="Services"
            expandToViewport
            onItemClick={({ detail }) => {
              const service = services.find((item) => item.id === detail.id)
              if (service) navigate(service.basePath)
            }}
          >
            Services
          </ButtonDropdown>
          <Link href="/" variant="secondary" fontSize="heading-s">
            Operator
          </Link>
        </div>
        <ButtonDropdown
          items={[
            { id: "account", text: "Account settings" },
            { id: "logout", text: "Log out" },
          ]}
          ariaLabel="Account"
          expandToViewport
          onItemClick={({ detail }) => {
            if (detail.id === "account") navigate("/account")
            if (detail.id === "logout") void logout()
          }}
        >
          {session.email}
        </ButtonDropdown>
      </div>
    </TopNavigation>
  )
}

export function Well() {
  return (
    <div id="well" className="shell-page">
      <Box color="text-body-secondary">No services.</Box>
    </div>
  )
}
