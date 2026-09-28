import { Component, useEffect, useState, type ComponentType, type ReactNode } from "react"
import Alert from "@cloudscape-design/components/alert"
import Box from "@cloudscape-design/components/box"
import Spinner from "@cloudscape-design/components/spinner"

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

  if (loaded.error) return <ModuleNote>{loaded.error}</ModuleNote>
  if (!loaded.Comp) {
    return (
      <div className="shell-center">
        <Spinner size="large" />
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

function ModuleNote({ children }: { children: string }) {
  return (
    <div className="shell-page">
      <Alert type="error">{children}</Alert>
    </div>
  )
}

class ModuleBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError(): { failed: boolean } {
    return { failed: true }
  }

  render() {
    if (this.state.failed) {
      return (
        <div className="shell-page">
          <Alert type="error">
            <Box>Module failed.</Box>
          </Alert>
        </div>
      )
    }
    return this.props.children
  }
}
