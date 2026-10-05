import { Link, useParams } from "react-router-dom"
import { Alert, Breadcrumb, Card } from "antd"
import type { TableColumnsType } from "antd"
import { seg, useList, type Membership } from "./api"
import { useBase } from "./base"
import { Keys } from "./keys"
import { DataTable, Id, More, Page, PageHeader, Status } from "./ui"

export function UserPage() {
  const { userId = "" } = useParams()
  const base = useBase()
  const memberships = useList<Membership>(`/users/${seg(userId)}/memberships`, "memberships")

  const columns: TableColumnsType<Membership> = [
    {
      title: "Organization",
      key: "org",
      render: (_, item) => <Link to={`${base}/organizations/${seg(item.organization.id)}`}>{item.organization.name}</Link>,
    },
    { title: "Slug", key: "slug", render: (_, item) => item.organization.slug },
    { title: "Status", dataIndex: "status", render: (status: string) => <Status value={status} /> },
    {
      title: "Member",
      dataIndex: "id",
      render: (id: string) => <Link to={`${base}/members/${seg(id)}`}>{id}</Link>,
    },
  ]

  return (
    <Page>
      <Breadcrumb items={[{ title: <Link to={base}>Identity</Link> }, { title: "User" }]} />
      <PageHeader title="User" description={<Id value={userId} />} />
      {memberships.error ? <Alert type="error" showIcon title={memberships.error} /> : null}
      <Card title="Memberships">
        <DataTable loading={memberships.loading} items={memberships.items} columns={columns} />
        <More hasMore={memberships.hasMore} onMore={memberships.loadMore} />
      </Card>
      <Keys path={`/users/${seg(userId)}/api-keys`} />
    </Page>
  )
}
