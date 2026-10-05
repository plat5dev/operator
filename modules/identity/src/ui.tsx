import { useState, type ReactNode } from "react"
import { Alert, Button, Empty, Flex, Modal, Spin, Table, Tag, Typography } from "antd"
import type { TableColumnsType } from "antd"

export function Page({ children }: { children: ReactNode }) {
  return (
    <div className="identity-page">
      <Flex vertical gap={16}>
        {children}
      </Flex>
    </div>
  )
}

export function PageHeader({
  title,
  description,
  extra,
}: {
  title: ReactNode
  description?: ReactNode
  extra?: ReactNode
}) {
  return (
    <Flex align="flex-start" justify="space-between" gap={16} wrap>
      <div>
        <Typography.Title level={3} style={{ margin: 0 }}>
          {title}
        </Typography.Title>
        {description ? <div className="identity-header-note">{description}</div> : null}
      </div>
      {extra}
    </Flex>
  )
}

export function Load({ loading, error, children }: { loading: boolean; error: string; children: ReactNode }) {
  if (loading) {
    return (
      <div className="identity-center">
        <Spin size="large" />
      </div>
    )
  }
  if (error) return <Alert type="error" showIcon title={error} />
  return (
    <Flex vertical gap={16}>
      {children}
    </Flex>
  )
}

export function More({ hasMore, onMore }: { hasMore: boolean; onMore: () => Promise<void> }) {
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  if (!hasMore && !error) return null

  async function run() {
    setBusy(true)
    setError("")
    try {
      await onMore()
    } catch (err) {
      setError(err instanceof Error ? err.message : "Request failed.")
    } finally {
      setBusy(false)
    }
  }

  return (
    <Flex vertical gap={8} className="identity-more">
      {error ? <Alert type="error" showIcon title={error} /> : null}
      {hasMore ? (
        <Button loading={busy} onClick={() => void run()}>
          Load more
        </Button>
      ) : null}
    </Flex>
  )
}

export function Secret({ label, value }: { label: string; value: string }) {
  return (
    <Alert
      type="info"
      showIcon
      title={label}
      description={
        <Typography.Text className="identity-secret" copyable>
          {value}
        </Typography.Text>
      }
    />
  )
}

export function EmptyNote() {
  return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="None." />
}

const statusColor: Record<string, string> = {
  active: "success",
  suspended: "warning",
  revoked: "default",
  expired: "default",
  redeemed: "processing",
}

export function Status({ value }: { value: string }) {
  return <Tag color={statusColor[value] ?? "default"}>{value}</Tag>
}

export function Id({ value }: { value: string }) {
  return (
    <Typography.Text className="identity-id" copyable={{ text: value }}>
      {value}
    </Typography.Text>
  )
}

export function DataTable<T extends { id: string }>({
  loading,
  items,
  columns,
}: {
  loading: boolean
  items: T[]
  columns: TableColumnsType<T>
}) {
  return (
    <Table<T>
      rowKey="id"
      size="middle"
      pagination={false}
      loading={loading}
      dataSource={items}
      columns={columns}
      scroll={{ x: "max-content" }}
      locale={{ emptyText: <EmptyNote /> }}
    />
  )
}

export function confirmDanger(title: string, okText: string, onOk: () => Promise<void>, content?: string) {
  Modal.confirm({
    title,
    content,
    okText,
    okButtonProps: { danger: true },
    cancelText: "Cancel",
    onOk,
  })
}
