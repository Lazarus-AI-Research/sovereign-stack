import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { PoweredBy } from "./PoweredBy";
import { locale } from "./i18n";
import "./styles.css";

document.documentElement.lang = locale;

// PoweredBy is a sibling of App at the root, not inside any page or route, so
// the attribution renders on every screen (login, loading, every tab) from one
// mount point. Do not move it into a page — a required branding position (see
// BRANDING.md).
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
    <PoweredBy />
  </StrictMode>,
);
