import { Component, useEffect, useState, type ComponentType, type ReactNode } from "react"
import { Alert, Spin } from "antd"

type Loaded = { Comp?: ComponentType; error?: string }

export function ModuleHost({ entry }: { entry: string }) {
  const [loaded, setLoaded] = useState<Loaded>({})

  useEffect(() => {
    let cancelled = false
    setLoaded({})
    import(/* @vite-ignore */ entry)
      .then((mod: { default?: unknown }) => {
        if (cancelled) return
        if (typeof mod.default !== "function") {
          setLoaded({ error: "Module did not export a component." })
          return
        }
        setLoaded({ Comp: mod.default as ComponentType })
      })
      .catch(() => {
        if (!cancelled) setLoaded({ error: "Module failed to load." })
      })
    return () => {
      cancelled = true
    }
  }, [entry])

  if (loaded.error) return <Alert type="error" showIcon title={loaded.error} />
  if (!loaded.Comp) {
    return (
      <div className="shell-center">
        <Spin size="large" />
      </div>
    )
  }
  const Comp = loaded.Comp
  return (
    <ModuleBoundary key={entry}>
      <Comp />
    </ModuleBoundary>
  )
}

class ModuleBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError(): { failed: boolean } {
    return { failed: true }
  }

  render() {
    if (this.state.failed) return <Alert type="error" showIcon title="Module failed." />
    return this.props.children
  }
}
