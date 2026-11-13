import { createContext, lazy, Suspense, useContext, useEffect, useState } from "react";
import { createBrowserRouter, Navigate, Outlet, RouterProvider } from "react-router-dom";
import { AuthProvider } from "contexts/AuthContext";
import { DialogProvider } from "contexts/DialogContext";
import { ToastProvider } from "contexts/ToastContext";
import { I18nProvider } from "i18n";
import AppLayout from "components/layout/AppLayout";
import Login from "pages/Login";
import Browser from "pages/Browser";
import { Loader2 } from "lucide-react";
import Viewer from "pages/Viewer";

// The editor pulls CodeMirror in: loaded only when a file is opened.
const Editor = lazy(() => import("pages/Editor"));
const Lazy = ({ children }) => (
  <Suspense
    fallback={
      <div className="flex flex-1 items-center justify-center text-muted-foreground">
        <Loader2 className="h-6 w-6 animate-spin" />
      </div>
    }
  >
    {children}
  </Suspense>
);
import Account from "pages/Account";

const THEME_KEY = "nullops.theme";

// The router is created once; the theme reaches routes through this context.
const ThemeContext = createContext({ darkMode: true, setDarkMode: () => {} });

function initialDark() {
  try {
    const v = localStorage.getItem(THEME_KEY);
    if (v) return v === "dark";
  } catch {
    /* storage unavailable */
  }
  return true;
}

function Root() {
  return (
    <I18nProvider>
      <ToastProvider>
        <AuthProvider>
          <DialogProvider>
            <Outlet />
          </DialogProvider>
        </AuthProvider>
      </ToastProvider>
    </I18nProvider>
  );
}

function LoginRoute() {
  return <Login {...useContext(ThemeContext)} />;
}

function LayoutRoute() {
  return <AppLayout {...useContext(ThemeContext)} />;
}

const router = createBrowserRouter([
  {
    element: <Root />,
    children: [
      { path: "/login", element: <LoginRoute /> },
      {
        element: <LayoutRoute />,
        children: [
          { path: "/files/*", element: <Browser /> },
          // Object detail (the editor) always gets its own route, never a modal.
          { path: "/edit/*", element: <Lazy><Editor /></Lazy> },
          { path: "/view/*", element: <Viewer /> },
          { path: "/account", element: <Account /> },
        ],
      },
      { path: "*", element: <Navigate to="/files" replace /> },
    ],
  },
]);

export default function App() {
  const [darkMode, setDarkMode] = useState(initialDark);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", darkMode);
    try {
      localStorage.setItem(THEME_KEY, darkMode ? "dark" : "light");
    } catch {
      /* ignore */
    }
  }, [darkMode]);

  return (
    <ThemeContext.Provider value={{ darkMode, setDarkMode }}>
      <RouterProvider router={router} />
    </ThemeContext.Provider>
  );
}
