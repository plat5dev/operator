import { createContext, useContext, useState, type ReactNode } from "react"
import { ConfigProvider, theme as antdTheme } from "antd"

export type Theme = "dark" | "light"

const storageKey = "operator-theme"
const eventName = "operator-theme"

// shell.css mirrors these. Ant Design hardcodes the header to #001529, so the
// Layout tokens have to carry the same colors or the bar ignores the theme.
const chrome = {
  dark: { header: "#141414", body: "#000000" },
  light: { header: "#ffffff", body: "#f5f5f5" },
} as const

export function readTheme(): Theme {
  try {
    return localStorage.getItem(storageKey) === "light" ? "light" : "dark"
  } catch {
    return "dark"
  }
}

const ThemeContext = createContext<{ theme: Theme; toggleTheme: () => void } | null>(null)

export function useTheme() {
  const value = useContext(ThemeContext)
  if (!value) throw new Error("theme missing")
  return value
}

export function ThemeRoot({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(() => {
    const initial = readTheme()
    document.documentElement.dataset.theme = initial
    return initial
  })

  function toggleTheme() {
    const next: Theme = theme === "dark" ? "light" : "dark"
    try {
      localStorage.setItem(storageKey, next)
    } catch {
      // Still applied for this page.
    }
    document.documentElement.dataset.theme = next
    window.dispatchEvent(new Event(eventName))
    setTheme(next)
  }

  const colors = chrome[theme]

  return (
    <ThemeContext.Provider value={{ theme, toggleTheme }}>
      <ConfigProvider
        theme={{
          algorithm: theme === "dark" ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
          components: {
            Layout: {
              headerBg: colors.header,
              bodyBg: colors.body,
              headerHeight: 56,
              headerPadding: "0 24px",
            },
          },
        }}
      >
        {children}
      </ConfigProvider>
    </ThemeContext.Provider>
  )
}
