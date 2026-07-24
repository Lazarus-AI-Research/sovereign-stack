// Minimal localization boundary. Product strings can move into per-locale
// dictionaries incrementally without changing route IDs or API values.
const dictionaries: Record<string, Record<string, string>> = {
  en: {
    "nav.Chat": "Chat",
    "nav.Activity": "Activity",
    "nav.Tools": "Tools",
    "nav.System": "System status",
    "nav.Models": "Models",
    "nav.Embeddings": "Embeddings",
    "nav.Evaluations": "Evaluations",
    "nav.People": "People",
    "nav.API & Providers": "API & Providers",
    "nav.Network Access": "Network Access",
    "nav.Backups & Recovery": "Backups & Recovery",
    "nav.Updates": "Updates",
    "nav.Settings": "Settings",
    "shell.administration": "Administration",
    "shell.signOut": "Sign out",
  },
};

export const locale = (navigator.languages.find((value) => dictionaries[value.split("-")[0]]) || "en").split("-")[0];

export function t(key: string, fallback: string) {
  return dictionaries[locale]?.[key] ?? dictionaries.en[key] ?? fallback;
}
