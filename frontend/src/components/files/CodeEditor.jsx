import { useEffect, useRef } from "react";
import { EditorView, basicSetup } from "codemirror";
import { Compartment, EditorState } from "@codemirror/state";
import { keymap } from "@codemirror/view";
import { indentWithTab } from "@codemirror/commands";
import { StreamLanguage } from "@codemirror/language";
import { oneDark } from "@codemirror/theme-one-dark";

// Language support, loaded on demand (each one becomes its own chunk).
const legacy = (load, name) => () => load().then((m) => StreamLanguage.define(m[name]));
const LANGS = [
  [/\.(m?jsx?|cjs)$/i, () => import("@codemirror/lang-javascript").then((m) => m.javascript({ jsx: true }))],
  [/\.tsx?$/i, () => import("@codemirror/lang-javascript").then((m) => m.javascript({ jsx: true, typescript: true }))],
  [/\.json$/i, () => import("@codemirror/lang-json").then((m) => m.json())],
  [/\.html?$/i, () => import("@codemirror/lang-html").then((m) => m.html())],
  [/\.(css|scss|less)$/i, () => import("@codemirror/lang-css").then((m) => m.css())],
  [/\.(xml|svg|csproj|xaml|plist)$/i, () => import("@codemirror/lang-xml").then((m) => m.xml())],
  [/\.py$/i, () => import("@codemirror/lang-python").then((m) => m.python())],
  [/\.(md|markdown)$/i, () => import("@codemirror/lang-markdown").then((m) => m.markdown())],
  [/\.sql$/i, () => import("@codemirror/lang-sql").then((m) => m.sql())],
  [/\.ya?ml$/i, () => import("@codemirror/lang-yaml").then((m) => m.yaml())],
  [/\.go$/i, () => import("@codemirror/lang-go").then((m) => m.go())],
  [/\.(sh|bash|zsh|ksh)$/i, legacy(() => import("@codemirror/legacy-modes/mode/shell"), "shell")],
  [/\.ps[dm]?1$/i, legacy(() => import("@codemirror/legacy-modes/mode/powershell"), "powerShell")],
  [/\.(vbs|vba|bas)$/i, legacy(() => import("@codemirror/legacy-modes/mode/vbscript"), "vbScript")],
  [/\.toml$/i, legacy(() => import("@codemirror/legacy-modes/mode/toml"), "toml")],
  [/\.(ini|conf|cfg|properties|env|reg|bat|cmd)$/i, legacy(() => import("@codemirror/legacy-modes/mode/properties"), "properties")],
  [/(^|\/)dockerfile$/i, legacy(() => import("@codemirror/legacy-modes/mode/dockerfile"), "dockerFile")],
  [/\.(c|h)$/i, legacy(() => import("@codemirror/legacy-modes/mode/clike"), "c")],
  [/\.(cpp|hpp|cc|cxx)$/i, legacy(() => import("@codemirror/legacy-modes/mode/clike"), "cpp")],
  [/\.java$/i, legacy(() => import("@codemirror/legacy-modes/mode/clike"), "java")],
  [/\.cs$/i, legacy(() => import("@codemirror/legacy-modes/mode/clike"), "csharp")],
  [/\.kt$/i, legacy(() => import("@codemirror/legacy-modes/mode/clike"), "kotlin")],
  [/\.rs$/i, legacy(() => import("@codemirror/legacy-modes/mode/rust"), "rust")],
  [/\.rb$/i, legacy(() => import("@codemirror/legacy-modes/mode/ruby"), "ruby")],
  [/\.lua$/i, legacy(() => import("@codemirror/legacy-modes/mode/lua"), "lua")],
  [/\.(pl|pm)$/i, legacy(() => import("@codemirror/legacy-modes/mode/perl"), "perl")],
];

export function languageFor(name) {
  for (const [re, load] of LANGS) if (re.test(name)) return load;
  return null;
}

const baseTheme = EditorView.theme({
  "&": { height: "100%", fontSize: "13px" },
  ".cm-scroller": { fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace" },
});

/**
 * CodeMirror 6 editor. `value` is only read on mount / when `docKey` changes
 * (a new document); edits flow out through onChange.
 */
export default function CodeEditor({ value, docKey, fileName, readOnly, dark, onChange }) {
  const host = useRef(null);
  const view = useRef(null);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const lang = useRef(new Compartment());
  const theme = useRef(new Compartment());
  const ro = useRef(new Compartment());

  useEffect(() => {
    const state = EditorState.create({
      doc: value,
      extensions: [
        basicSetup,
        keymap.of([indentWithTab]),
        baseTheme,
        lang.current.of([]),
        theme.current.of(dark ? oneDark : []),
        ro.current.of([EditorState.readOnly.of(!!readOnly), EditorView.editable.of(!readOnly)]),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) onChangeRef.current?.(u.state.doc.toString());
        }),
      ],
    });
    const v = new EditorView({ state, parent: host.current });
    view.current = v;
    let alive = true;
    const load = languageFor(fileName || "");
    if (load) {
      load()
        .then((ext) => alive && v.dispatch({ effects: lang.current.reconfigure(ext) }))
        .catch(() => {});
    }
    v.focus();
    return () => {
      alive = false;
      v.destroy();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [docKey]);

  useEffect(() => {
    view.current?.dispatch({ effects: theme.current.reconfigure(dark ? oneDark : []) });
  }, [dark]);

  useEffect(() => {
    view.current?.dispatch({
      effects: ro.current.reconfigure([EditorState.readOnly.of(!!readOnly), EditorView.editable.of(!readOnly)]),
    });
  }, [readOnly]);

  return <div ref={host} className="h-full min-h-0 w-full overflow-hidden" />;
}
