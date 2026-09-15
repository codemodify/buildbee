import { useState, type ReactNode } from "react";
import { api } from "../api";
import { desktopOn, desktopPossible, setDesktop } from "../desktop";
import { loadLook, saveLook, type Look } from "../look";
import { useMe } from "../me";
import { useLoad } from "../store";
import type { Preferences as Prefs } from "../types";
import { Button, ErrorNote, Sheet, Toggle, cx, inputClass } from "./kit";

/** PrefsButton opens your settings: name, look, notifications. */
export function PrefsButton() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)} className="rounded-md p-1.5 text-bb-muted hover:bg-bb-hover hover:text-bb-fg" aria-label="Your settings" title="Your settings">
        <svg width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
          <circle cx="9" cy="6" r="3" />
          <path d="M3 15.5c.8-2.8 3.2-4.5 6-4.5s5.2 1.7 6 4.5" />
        </svg>
      </button>
      {open && <PreferencesSheet onClose={() => setOpen(false)} />}
    </>
  );
}

function PreferencesSheet({ onClose }: { onClose: () => void }) {
  const me = useMe();
  const [name, setName] = useState(me?.name ?? "");
  const [look, setLook] = useState<Look>(loadLook);
  const { data: prefs, setData: setPrefs } = useLoad<Prefs>(() => api.preferences(), []);
  const [err, setErr] = useState("");
  const [desktop, setDesktopState] = useState(desktopOn);
  const change = (next: Look) => {
    setLook(next);
    saveLook(next);
  };
  async function rename() {
    setErr("");
    try {
      await api.rename(name.trim());
      window.location.reload(); // every view shows the new name
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }
  async function mute(patch: { mute_mentions?: boolean; mute_routines?: boolean }) {
    try {
      setPrefs(await api.setPreferences(patch));
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }
  return (
    <Sheet title="Your settings" onClose={onClose}>
      <div className="space-y-5">
        <Section title="Name">
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              void rename();
            }}
          >
            <input id="pref-name" className={inputClass} value={name} onChange={(e) => setName(e.target.value)} aria-label="Your name" />
            <Button type="submit" disabled={!name.trim() || name.trim() === me?.name}>
              Save
            </Button>
          </form>
        </Section>
        <Section title="Theme">
          <Segmented
            value={look.theme}
            options={[
              ["system", "System"],
              ["light", "Light"],
              ["dark", "Dark"],
            ]}
            onChange={(theme) => change({ ...look, theme })}
          />
        </Section>
        <Section title="Corners">
          <Segmented
            value={look.corners}
            options={[
              ["rounded", "Rounded"],
              ["sharp", "Sharp"],
            ]}
            onChange={(corners) => change({ ...look, corners })}
          />
        </Section>
        <Section title="Notifications">
          <label className="flex items-center justify-between text-[13.5px]">
            <span>
              On this computer
              {!desktopPossible && <span className="block text-[12px] text-bb-subtle">Browsers allow it only over HTTPS or on localhost.</span>}
            </span>
            <Toggle label="Desktop notifications" checked={desktop} onChange={(v) => void setDesktop(v).then(setDesktopState)} />
          </label>
          <label className="flex items-center justify-between text-[13.5px]">
            Mentions and DMs
            <Toggle label="Mentions" checked={!prefs?.mute_mentions} onChange={(v) => void mute({ mute_mentions: !v })} />
          </label>
          <label className="flex items-center justify-between text-[13.5px]">
            Routines
            <Toggle label="Routines" checked={!prefs?.mute_routines} onChange={(v) => void mute({ mute_routines: !v })} />
          </label>
        </Section>
        <ErrorNote>{err}</ErrorNote>
        <div className="flex items-center justify-between border-t border-bb-border pt-3 text-[12.5px] text-bb-subtle">
          <span>Theme and corners are kept in this browser.</span>
          <button
            type="button"
            className="rounded px-1.5 py-0.5 hover:bg-bb-hover hover:text-bb-fg"
            onClick={async () => {
              await api.forgetMe();
              window.location.reload();
            }}
          >
            Switch person
          </button>
        </div>
      </div>
    </Sheet>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-[12px] font-semibold tracking-wide text-bb-subtle uppercase">{title}</h3>
      {children}
    </section>
  );
}

function Segmented<T extends string>({ value, options, onChange }: { value: T; options: [T, string][]; onChange: (v: T) => void }) {
  return (
    <div className="inline-flex rounded-md border border-bb-border p-0.5" role="radiogroup">
      {options.map(([v, label]) => (
        <button
          key={v}
          type="button"
          role="radio"
          aria-checked={v === value}
          onClick={() => onChange(v)}
          className={cx("rounded px-3 py-1 text-[13px]", v === value ? "bg-bb-accent-soft font-medium text-bb-fg" : "text-bb-muted hover:text-bb-fg")}
        >
          {label}
        </button>
      ))}
    </div>
  );
}
