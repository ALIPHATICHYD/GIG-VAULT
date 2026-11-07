import { useEffect, useState } from "react";

export type Theme = "light" | "dark";
const KEY = "gigvault.theme";

export function useTheme() {
  const [theme, setTheme] = useState<Theme>(() => (document.documentElement.dataset.theme as Theme) || "light");
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    document.querySelector('meta[name="theme-color"]')?.setAttribute("content", theme === "dark" ? "#0E1110" : "#EDF1ED");
  }, [theme]);
  const toggle = () => setTheme((t) => { const n = t === "dark" ? "light" : "dark"; localStorage.setItem(KEY, n); return n; });
  return { theme, toggle };
}
