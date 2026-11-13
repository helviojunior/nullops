import { createContext, useContext, useState, useCallback, useEffect } from "react";
import { LOCALES } from "./locales";

export const SUPPORTED = ["en", "pt-br"];
export const FALLBACK = "en";
const STORAGE_KEY = "nullops.lang";

export const LANGUAGE_OPTIONS = [
  { value: "en", label: "English" },
  { value: "pt-br", label: "Português (Brasil)" },
];

// EN is the default language and the fallback of every translation. The
// browser language is tried first; anything unsupported falls back to EN.
export function normalize(lang) {
  const value = (lang || "").trim().toLowerCase().replace("_", "-");
  if (SUPPORTED.includes(value)) return value;
  const base = value.split("-")[0];
  if (base === "pt") return "pt-br";
  if (base === "en") return "en";
  return null;
}

export function detectBrowserLanguage() {
  const candidates =
    navigator.languages && navigator.languages.length ? navigator.languages : [navigator.language];
  for (const c of candidates) {
    const n = normalize(c);
    if (n) return n;
  }
  return FALLBACK;
}

function initialLanguage() {
  try {
    const saved = normalize(localStorage.getItem(STORAGE_KEY));
    if (saved) return saved;
  } catch {
    /* storage unavailable */
  }
  return detectBrowserLanguage();
}

const I18nContext = createContext(null);

export function I18nProvider({ children }) {
  const [lang, setLang] = useState(initialLanguage);

  const setLanguage = useCallback((value) => {
    const next = normalize(value) || FALLBACK;
    setLang(next);
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      /* ignore */
    }
  }, []);

  useEffect(() => {
    document.documentElement.setAttribute("lang", lang);
  }, [lang]);

  const t = useCallback(
    (key, fallbackText) => {
      const table = LOCALES[lang] || LOCALES[FALLBACK];
      return table[key] ?? LOCALES[FALLBACK][key] ?? fallbackText ?? key;
    },
    [lang]
  );

  // Simple interpolation: tf("x.y", { n: 1 }) replaces {n}.
  const tf = useCallback(
    (key, params = {}, fallbackText) => {
      const text = t(key, fallbackText);
      return Object.keys(params).reduce((acc, k) => acc.replaceAll(`{${k}}`, params[k]), text);
    },
    [t]
  );

  return (
    <I18nContext.Provider value={{ lang, setLanguage, t, tf }}>{children}</I18nContext.Provider>
  );
}

export function useI18n() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n must be used within I18nProvider");
  return ctx;
}
