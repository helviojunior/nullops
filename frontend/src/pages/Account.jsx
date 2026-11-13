import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ArrowLeft, Loader2, ShieldCheck, ShieldOff, Smartphone } from "lucide-react";
import { useApi, useAuth } from "contexts/AuthContext";
import { useDialog } from "contexts/DialogContext";
import { useToast } from "contexts/ToastContext";
import { useI18n } from "i18n";
import { Button } from "components/ui/button";
import { Card, CardContent, CardHeader } from "components/ui/card";
import { Input } from "components/ui/input";
import { Label } from "components/ui/label";
import { errorText } from "lib/errors";

// Account security (own route): optional MFA enrollment for the signed-in user.
export default function Account() {
  const run = useApi();
  const navigate = useNavigate();
  const { user } = useAuth();
  const { prompt } = useDialog();
  const toast = useToast();
  const { t, lang } = useI18n();
  const [enabled, setEnabled] = useState(null);
  const [setup, setSetup] = useState(null); // {secret, uri, qr}
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!user?.auth) return;
    run((c) => c.call("mfa_status", {}))
      .then((r) => setEnabled(!!r.enabled))
      .catch((e) => e.status !== 401 && setError(errorText(e, lang)));
  }, [run, user, lang]);

  const start = async () => {
    setBusy(true);
    setError("");
    try {
      setSetup(await run((c) => c.call("mfa_setup", {})));
      setCode("");
    } catch (e) {
      if (e.status !== 401) setError(errorText(e, lang));
    } finally {
      setBusy(false);
    }
  };

  const confirm = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await run((c) => c.call("mfa_enable", { code: code.trim() }));
      setSetup(null);
      setEnabled(true);
      toast.success(t("account.enabled"));
    } catch (err) {
      if (err.status !== 401) setError(errorText(err, lang));
    } finally {
      setBusy(false);
    }
  };

  const disable = () =>
    prompt({
      title: t("account.disableTitle"),
      label: t("login.mfaCode"),
      hint: t("account.disableHint"),
      variant: "warning",
      confirmLabel: t("account.disable"),
      validate: (v) => (/^\d{6}$/.test(v.trim()) ? "" : t("login.invalidCode")),
      onSubmit: async (v) => {
        await run((c) => c.call("mfa_disable", { code: v.trim() })).catch((err) => {
          throw new Error(errorText(err, lang));
        });
      },
    }).then((v) => {
      if (v === null) return;
      setEnabled(false);
      toast.success(t("account.disabled"));
    });

  return (
    <div className="w-full flex-1 overflow-auto p-4 md:p-6">
      <div className="mb-4 flex items-center gap-2">
        <Button variant="ghost" size="sm" onClick={() => navigate(-1)}>
          <ArrowLeft className="h-4 w-4" /> {t("common.back")}
        </Button>
        <h1 className="text-lg font-semibold">{t("account.title")}</h1>
        <span className="text-sm text-muted-foreground">· {user?.username}</span>
      </div>

      <Card className="w-full">
        <CardHeader>
          <div className="flex items-center gap-2 font-medium">
            <Smartphone className="h-5 w-5 text-primary" /> {t("account.mfa")}
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {!user?.auth ? (
            <p className="text-sm text-muted-foreground">{t("account.noAuth")}</p>
          ) : enabled === null ? (
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          ) : (
            <>
              <div
                className={`flex items-center gap-3 rounded-lg border p-3 text-sm ${
                  enabled ? "border-emerald-500/40 bg-emerald-500/10" : "border-border"
                }`}
              >
                {enabled ? (
                  <ShieldCheck className="h-5 w-5 text-emerald-500" />
                ) : (
                  <ShieldOff className="h-5 w-5 text-muted-foreground" />
                )}
                <span className="flex-1">{enabled ? t("account.mfaOn") : t("account.mfaOff")}</span>
                {enabled ? (
                  <Button variant="outline" size="sm" onClick={disable}>
                    {t("account.disable")}
                  </Button>
                ) : (
                  !setup && (
                    <Button size="sm" onClick={start} loading={busy}>
                      {t("account.enable")}
                    </Button>
                  )
                )}
              </div>

              {setup && (
                <div className="grid gap-6 md:grid-cols-[auto,1fr]">
                  <img
                    src={setup.qr}
                    alt="QR code"
                    className="h-56 w-56 rounded-lg border border-border bg-white p-2"
                  />
                  <form onSubmit={confirm} className="space-y-4">
                    <p className="text-sm">{t("account.scan")}</p>
                    <div className="space-y-1">
                      <Label>{t("account.key")}</Label>
                      <code className="block break-all rounded-md bg-muted px-3 py-2 font-mono text-sm">
                        {setup.secret.replace(/(.{4})/g, "$1 ").trim()}
                      </code>
                    </div>
                    <p className="text-sm">{t("account.confirm")}</p>
                    <Input
                      inputMode="numeric"
                      autoComplete="one-time-code"
                      maxLength={7}
                      autoFocus
                      required
                      value={code}
                      onChange={(e) => setCode(e.target.value.replace(/[^0-9 ]/g, ""))}
                      className="max-w-full text-center font-mono text-lg tracking-[0.4em] md:max-w-xs"
                      error={!!error}
                    />
                    <div className="flex gap-2">
                      <Button type="submit" loading={busy}>
                        {t("account.enable")}
                      </Button>
                      <Button type="button" variant="outline" onClick={() => setSetup(null)} disabled={busy}>
                        {t("account.cancelSetup")}
                      </Button>
                    </div>
                  </form>
                </div>
              )}
              {error && <p className="text-sm text-red-500">{error}</p>}
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
