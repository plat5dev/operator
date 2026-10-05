import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { BrowserRouter } from "react-router-dom"
import { App as AntApp } from "antd"
import "antd/dist/reset.css"
import App from "./App"
import "./shell.css"
import { ThemeRoot } from "./theme"

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeRoot>
      <AntApp>
        <BrowserRouter>
          <App />
        </BrowserRouter>
      </AntApp>
    </ThemeRoot>
  </StrictMode>,
)
