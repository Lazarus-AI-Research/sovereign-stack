// Runtime theming: apply the customer's branding to the live UI.
//
// This is the "easy to change the colors" half of the branding policy (see
// BRANDING.md). Colors set in Settings → Branding are pushed onto CSS custom
// properties here, so the whole UI re-skins with no rebuild. The "Powered by
// Lazarus AI" attribution is deliberately NOT part of this — it is a build-time
// constant and nothing here can touch it.

export interface Theme {
  product_name?: string;
  company_name?: string;
  logo?: string;
  favicon?: string;
  colors?: { primary?: string; accent?: string };
}

// Only #rgb / #rrggbb are accepted, so a bad or hostile value can never inject
// arbitrary CSS through setProperty.
const HEX = /^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/;

function hexToRgba(hex: string, alpha: number): string | null {
  let h = hex.replace("#", "");
  if (h.length === 3) h = h.split("").map((c) => c + c).join("");
  if (h.length !== 6) return null;
  const r = parseInt(h.slice(0, 2), 16);
  const g = parseInt(h.slice(2, 4), 16);
  const b = parseInt(h.slice(4, 6), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

export function applyTheme(theme: Theme | null | undefined): void {
  if (!theme) return;
  const root = document.documentElement;

  const accent = theme.colors?.accent;
  if (accent && HEX.test(accent)) {
    root.style.setProperty("--accent", accent);
    const soft = hexToRgba(accent, 0.12);
    if (soft) root.style.setProperty("--accent-soft", soft);
  }

  const primary = theme.colors?.primary;
  if (primary && HEX.test(primary)) {
    root.style.setProperty("--primary", primary);
  }

  if (theme.product_name) {
    document.title = `${theme.product_name} — Control`;
  }

  if (theme.favicon) {
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!link) {
      link = document.createElement("link");
      link.rel = "icon";
      document.head.appendChild(link);
    }
    link.href = theme.favicon;
  }
}
