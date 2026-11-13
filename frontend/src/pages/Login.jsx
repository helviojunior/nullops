import { useState } from "react";
import { Navigate } from "react-router-dom";
import { ArrowLeft, KeyRound, Loader2, ServerCrash, ShieldAlert, UserRound } from "lucide-react";
import { useAuth } from "contexts/AuthContext";
import { useI18n } from "i18n";
import { Button } from "components/ui/button";
import { Input } from "components/ui/input";
import { Label } from "components/ui/label";
import { Card, CardContent, CardHeader } from "components/ui/card";
import { LanguageSelect, ThemeToggle } from "components/layout/AppLayout";
import Logo from "components/brand/Logo";

// Authentication screen: the documented exception to the 100%-width rule
// (centered 420px card, standard login layout).
export default function Login({ darkMode, setDarkMode }) {
  const { status, user, expired, encrypted, loginStart, loginMFA, loginPassword, connect } = useAuth();
  const { t, tf } = useI18n();
  // user -> [mfa] -> password
  const [step, setStep] = useState("user");
  const [username, setUsername] = useState("");
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  if (user) return <Navigate to="/files" replace />;

  const disabled = status !== "ready" || busy;

  const restart = () => {
    setStep("user");
    setCode("");
    setPassword("");
    setError("");
  };

  const showError = (err) => {
    if (err.retryAfter) setError(tf("login.throttled", { s: err.retryAfter }));
    else if (err.message === "Invalid code") setError(t("login.invalidCode"));
    else if (err.message === "Invalid credentials") setError(t("login.invalid"));
    else if (err.message === "Login expired") {
      restart();
      setError(t("login.stepExpired"));
    } else setError(err.message);
  };

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (step === "user") {
        const next = await loginStart(username.trim());
        if (next !== "done") setStep(next);
      } else if (step === "mfa") {
        await loginMFA(code.trim());
        setStep("password");
      } else {
        await loginPassword(username.trim(), password);
      }
    } catch (err) {
      if (step === "password") setPassword("");
      if (step === "mfa") setCode("");
      showError(err);
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
      {/* centered, a bit above the middle on taller screens */}
      <div className="flex flex-1 items-center justify-center p-4 md:pb-[18vh]">
        <Card className="w-full max-w-[420px] animate-fade-in">
          <CardHeader className="items-center text-center">
            <Logo className="mb-3 h-14" alt={t("app.name")} />
            <h1 className="text-lg font-semibold">{t("app.product")}</h1>
            <p className="text-sm text-muted-foreground">
              {encrypted ? t("login.subtitle") : t("login.subtitlePlain")}
            </p>
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
            {!encrypted && status === "ready" && (
              <div className="mb-4 rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-sm">
                <div className="mb-1 flex items-center gap-2 font-medium text-red-500">
                  <ShieldAlert className="h-4 w-4" /> {t("login.plainTitle")}
                </div>
                <p className="text-muted-foreground">{t("login.plainHint")}</p>
              </div>
            )}
            {expired && status === "ready" && (
              <div className="mb-4 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-sm text-amber-500">
                {t("login.expired")}
              </div>
            )}

            <form onSubmit={submit} className="space-y-4">
              {step !== "user" && (
                <div className="flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2 text-sm">
                  <UserRound className="h-4 w-4 text-muted-foreground" />
                  <span className="flex-1 truncate font-medium">{username}</span>
                  <button
                    type="button"
                    onClick={restart}
                    className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                  >
                    <ArrowLeft className="h-3.5 w-3.5" /> {t("login.changeUser")}
                  </button>
                </div>
              )}

              {step === "user" && (
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
                    error={!!error}
                  />
                </div>
              )}

              {step === "mfa" && (
                <div className="space-y-2">
                  <Label htmlFor="code" className="flex items-center gap-1.5">
                    <KeyRound className="h-4 w-4" /> {t("login.mfaCode")}
                  </Label>
                  <Input
                    id="code"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    pattern="[0-9 ]{6,7}"
                    maxLength={7}
                    autoFocus
                    required
                    value={code}
                    onChange={(e) => setCode(e.target.value.replace(/[^0-9 ]/g, ""))}
                    disabled={disabled}
                    error={!!error}
                    className="text-center font-mono text-lg tracking-[0.4em]"
                  />
                  <p className="text-xs text-muted-foreground">{t("login.mfaHint")}</p>
                </div>
              )}

              {step === "password" && (
                <div className="space-y-2">
                  {/* hidden username keeps password managers working */}
                  <input type="text" name="username" autoComplete="username" value={username} readOnly hidden />
                  <Label htmlFor="password">{t("login.password")}</Label>
                  <Input
                    id="password"
                    type="password"
                    autoComplete="current-password"
                    autoFocus
                    required
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    disabled={disabled}
                    error={!!error}
                  />
                </div>
              )}

              {error && <p className="text-sm text-red-500">{error}</p>}
              <Button type="submit" className="w-full" loading={busy} disabled={disabled}>
                {step === "password" ? t("login.submit") : t("login.next")}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
