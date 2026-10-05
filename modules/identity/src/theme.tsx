import { useEffect, useState, type ReactNode } from "react"
import { ConfigProvider, theme as antdTheme } from "antd"

type Theme = "dark" | "light"

function readTheme(): Theme {
  return document.documentElement.dataset.theme === "light" ? "light" : "dark"
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(readTheme)

  useEffect(() => {
    function onTheme() {
      setTheme(readTheme())
    }
    window.addEventListener("operator-theme", onTheme)
    return () => window.removeEventListener("operator-theme", onTheme)
  }, [])

  return (
    <ConfigProvider theme={{ algorithm: theme === "dark" ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm }}>
      {children}
    </ConfigProvider>
  )
}
