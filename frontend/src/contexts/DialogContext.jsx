import { createContext, useCallback, useContext, useRef, useState } from "react";
import { AlertTriangle, CheckCircle2, Info, Trash2, PencilLine } from "lucide-react";
import { Modal } from "components/ui/modal";
import { Button } from "components/ui/button";
import { Input } from "components/ui/input";
import { Label } from "components/ui/label";
import { SwitchField } from "components/ui/switch";
import { useI18n } from "i18n";

/**
 * Replaces window.alert / window.confirm / window.prompt.
 *
 *   const { confirm, alert, prompt } = useDialog();
 *   if (await confirm({ title: "Delete?", variant: "danger" })) { ... }
 *   const name = await prompt({ title: "New folder", label: "Name" }); // null = cancelled
 *
 * `onConfirm` (confirm) / `onSubmit` (prompt) keep the modal open with a
 * spinner while the action runs; if they throw, the modal stays open and the
 * error message is shown inline.
 */
const DialogContext = createContext(null);

const variantIcons = {
  danger: { Icon: Trash2, wrapper: "bg-red-100 dark:bg-red-900/30", icon: "text-red-600 dark:text-red-400" },
  warning: { Icon: AlertTriangle, wrapper: "bg-amber-100 dark:bg-amber-900/30", icon: "text-amber-600 dark:text-amber-400" },
  success: { Icon: CheckCircle2, wrapper: "bg-emerald-100 dark:bg-emerald-900/30", icon: "text-emerald-600 dark:text-emerald-400" },
  info: { Icon: Info, wrapper: "bg-blue-100 dark:bg-blue-900/30", icon: "text-blue-600 dark:text-blue-400" },
  input: { Icon: PencilLine, wrapper: "bg-blue-100 dark:bg-blue-900/30", icon: "text-blue-600 dark:text-blue-400" },
};

function DialogIcon({ variant }) {
  const config = variantIcons[variant] || variantIcons.info;
  const { Icon } = config;
  return (
    <div className={`w-12 h-12 rounded-full flex items-center justify-center ${config.wrapper}`}>
      <Icon className={`h-5 w-5 ${config.icon}`} />
    </div>
  );
}

export function DialogProvider({ children }) {
  const { t } = useI18n();
  const [dialog, setDialog] = useState(null);
  const [busy, setBusy] = useState(false);
  const [value, setValue] = useState("");
  const [toggle, setToggle] = useState(false);
  const [error, setError] = useState("");
  const resolverRef = useRef(null);

  const close = useCallback((result) => {
    setDialog(null);
    setBusy(false);
    setError("");
    if (resolverRef.current) {
      resolverRef.current(result);
      resolverRef.current = null;
    }
  }, []);

  const open = useCallback((config) => {
    return new Promise((resolve) => {
      resolverRef.current = resolve;
      setValue(config.defaultValue ?? "");
      setToggle(!!config.toggleDefault);
      setError("");
      setDialog(config);
    });
  }, []);

  const confirm = useCallback(
    (options = {}) =>
      open({ type: "confirm", variant: "warning", confirmLabel: t("common.confirm"), cancelLabel: t("common.cancel"), ...options }),
    [open, t]
  );

  const alert = useCallback(
    (options = {}) => open({ type: "alert", variant: "info", confirmLabel: t("common.ok"), ...options }),
    [open, t]
  );

  // Resolves with the entered string ({value, toggle} when `toggleLabel` is set), or null.
  const prompt = useCallback(
    (options = {}) =>
      open({ type: "prompt", variant: "input", confirmLabel: t("common.ok"), cancelLabel: t("common.cancel"), ...options }),
    [open, t]
  );

  const handleConfirm = async () => {
    const isPrompt = dialog?.type === "prompt";
    const result = isPrompt ? (dialog.toggleLabel ? { value, toggle } : value) : true;
    if (isPrompt && dialog.validate) {
      const msg = dialog.validate(value);
      if (msg) {
        setError(msg);
        return;
      }
    }
    const action = isPrompt ? dialog.onSubmit : dialog?.onConfirm;
    if (action) {
      setBusy(true);
      setError("");
      try {
        await action(result);
      } catch (err) {
        setBusy(false);
        setError(err?.message || t("common.error"));
        return;
      }
    }
    close(result);
  };

  const isAlert = dialog?.type === "alert";
  // confirm() may offer a third choice: resolves with "extra".
  const extra = dialog?.type === "confirm" && dialog.extraLabel;
  const isPrompt = dialog?.type === "prompt";

  return (
    <DialogContext.Provider value={{ confirm, alert, prompt }}>
      {children}
      <Modal
        open={!!dialog}
        onClose={busy ? undefined : () => close(isPrompt ? null : false)}
        title={dialog?.title}
        description={dialog?.description}
        icon={<DialogIcon variant={dialog?.variant} />}
        size={dialog?.size || (isPrompt || extra ? "md" : "sm")}
        footer={
          <>
            {!isAlert && (
              <Button variant="outline" onClick={() => close(isPrompt ? null : false)} disabled={busy}>
                {dialog?.cancelLabel}
              </Button>
            )}
            {extra && (
              <Button variant="outline" onClick={() => close("extra")} disabled={busy}>
                {dialog.extraLabel}
              </Button>
            )}
            <Button
              variant={dialog?.variant === "danger" ? "destructive" : "default"}
              onClick={handleConfirm}
              loading={busy}
              autoFocus={!isPrompt}
            >
              {dialog?.confirmLabel}
            </Button>
          </>
        }
      >
        {isPrompt && (
          <form
            className="mt-4 space-y-3 text-left"
            onSubmit={(e) => {
              e.preventDefault();
              if (!busy) handleConfirm();
            }}
          >
            {!dialog.noInput && (
              <>
            {dialog.label && <Label htmlFor="dialog-input">{dialog.label}</Label>}
            <Input
              id="dialog-input"
              autoFocus
              value={value}
              placeholder={dialog.placeholder}
              onChange={(e) => {
                setValue(e.target.value);
                setError("");
              }}
              onFocus={(e) => {
                // select the file stem, not the extension
                const v = e.target.value;
                const dot = v.lastIndexOf(".");
                if (dialog.selectStem && dot > 0) e.target.setSelectionRange(0, dot);
                else e.target.select();
              }}
              error={!!error}
              disabled={busy}
            />
            {dialog.hint && <p className="text-xs text-muted-foreground">{dialog.hint}</p>}
              </>
            )}
            {dialog.toggleLabel && (
              <SwitchField
                checked={toggle}
                onChange={setToggle}
                label={dialog.toggleLabel}
                description={dialog.toggleDescription}
                disabled={busy}
              />
            )}
          </form>
        )}
        {error && <p className="mt-3 text-sm text-red-500 text-left">{error}</p>}
      </Modal>
    </DialogContext.Provider>
  );
}

export function useDialog() {
  const ctx = useContext(DialogContext);
  if (!ctx) throw new Error("useDialog must be used within <DialogProvider>");
  return ctx;
}
