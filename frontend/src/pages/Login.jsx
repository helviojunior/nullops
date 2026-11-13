import { useState } from "react";
import { Navigate } from "react-router-dom";
import { FolderLock, Loader2, ServerCrash, ShieldAlert } from "lucide-react";
import { useAuth } from "contexts/AuthContext";
import { useI18n } from "i18n";
import { Button } from "components/ui/button";
import { Input } from "components/ui/input";
import { Label } from "components/ui/label";
import { Card, CardContent, CardHeader } from "components/ui/card";
import { LanguageSelect, ThemeToggle } from "components/layout/AppLayout";

// Authentication screen: the documented exception to the 100%-width rule
// (centered 420px card, standard login layout).
export default function Login({ darkMode, setDarkMode }) {
  const { status, user, expired, login, connect } = useAuth();
  const { t, tf } = useI18n();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  if (user) return <Navigate to="/files" replace />;

  const disabled = status !== "ready" || busy;

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await login(username, password);
    } catch (err) {
      setPassword("");
      if (err.retryAfter) setError(tf("login.throttled", { s: err.retryAfter }));
      else if (err.status === 200) setError(t("login.invalid"));
      else setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex min-h-screen flex-col bg-background">
      <div className="flex justify-end gap-2 p-4">
        <LanguageSelect />
        <ThemeToggle darkMode={darkMode} setDarkMode={setDarkMode} />
      </div>
      <div className="flex flex-1 items-center justify-center p-4">
        <Card className="w-full max-w-[420px] animate-fade-in">
          <CardHeader className="items-center text-center">
            <div className="mb-2 flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10">
              <FolderLock className="h-7 w-7 text-primary" />
            </div>
            <h1 className="text-xl font-semibold">{t("app.name")}</h1>
            <p className="text-sm text-muted-foreground">{t("login.subtitle")}</p>
          </CardHeader>
          <CardContent>
            {status === "insecure" && (
              <div className="mb-4 rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-sm">
                <div className="mb-1 flex items-center gap-2 font-medium text-red-500">
                  <ShieldAlert className="h-4 w-4" /> {t("login.insecureTitle")}
                </div>
                <p className="text-muted-foreground">{t("login.insecureHint")}</p>
              </div>
            )}
            {status === "error" && (
              <div className="mb-4 rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-sm">
                <div className="mb-1 flex items-center gap-2 font-medium text-red-500">
                  <ServerCrash className="h-4 w-4" /> {t("login.connectErrorTitle")}
                </div>
                <p className="mb-3 text-muted-foreground">{t("login.connectErrorHint")}</p>
                <Button variant="outline" size="sm" onClick={connect}>
                  {t("common.retry")}
                </Button>
              </div>
            )}
            {status === "connecting" && (
              <div className="mb-4 flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" /> {t("login.connecting")}
              </div>
            )}
            {expired && status === "ready" && (
              <div className="mb-4 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-sm text-amber-500">
                {t("login.expired")}
              </div>
            )}

            <form onSubmit={submit} className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="username">{t("login.username")}</Label>
                <Input
                  id="username"
                  autoComplete="username"
                  autoFocus
                  required
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  disabled={disabled}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password">{t("login.password")}</Label>
                <Input
                  id="password"
                  type="password"
                  autoComplete="current-password"
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  disabled={disabled}
                  error={!!error}
                />
              </div>
              {error && <p className="text-sm text-red-500">{error}</p>}
              <Button type="submit" className="w-full" loading={busy} disabled={disabled}>
                {t("login.submit")}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
