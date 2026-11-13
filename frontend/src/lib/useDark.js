import { useEffect, useState } from "react";

// Follows the "dark" class on <html> (set by the theme toggle).
export function useDark() {
  const get = () => document.documentElement.classList.contains("dark");
  const [dark, setDark] = useState(get);
  useEffect(() => {
    const mo = new MutationObserver(() => setDark(get()));
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    return () => mo.disconnect();
  }, []);
  return dark;
}
