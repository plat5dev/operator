import { Link, useParams } from "react-router-dom"
import Alert from "@cloudscape-design/components/alert"
import Header from "@cloudscape-design/components/header"
import SpaceBetween from "@cloudscape-design/components/space-between"
import Table from "@cloudscape-design/components/table"
import { seg, useList, type Membership } from "./api"
import { useBase } from "./base"
import { Keys } from "./keys"
import { Empty, More, Page } from "./ui"

export function UserPage() {
  const { userId = "" } = useParams()
  const base = useBase()
  const memberships = useList<Membership>(`/users/${seg(userId)}/memberships`, "memberships")

  return (
    <Page>
      <Header variant="h1" description={userId}>
        <Link to={base}>Identity</Link>
      </Header>
      <SpaceBetween size="l">
        <Header variant="h2">Memberships</Header>
        {memberships.error ? <Alert type="error">{memberships.error}</Alert> : null}
        <Table
          items={memberships.items}
          loading={memberships.loading}
          trackBy="id"
          empty={<Empty />}
          columnDefinitions={[
            {
              id: "org",
              header: "Organization",
              cell: (item) => (
                <Link to={`${base}/organizations/${seg(item.organization.id)}`}>{item.organization.name}</Link>
              ),
            },
            { id: "slug", header: "Slug", cell: (item) => item.organization.slug },
            { id: "status", header: "Status", cell: (item) => item.status },
            {
              id: "member",
              header: "Member",
              cell: (item) => <Link to={`${base}/members/${seg(item.id)}`}>{item.id}</Link>,
            },
          ]}
        />
        <More hasMore={memberships.hasMore} onMore={memberships.loadMore} />
        <Keys path={`/users/${seg(userId)}/api-keys`} />
      </SpaceBetween>
    </Page>
  )
}
