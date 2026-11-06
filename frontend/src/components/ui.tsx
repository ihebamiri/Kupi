import type { ButtonHTMLAttributes, ReactNode } from "react";
import { LoaderCircle } from "lucide-react";

type Variant = "primary" | "secondary" | "ghost" | "danger";

const VARIANTS: Record<Variant, string> = {
  primary: "bg-accent text-accent-fg shadow-sm shadow-accent/25 hover:brightness-110 active:brightness-95",
  secondary: "border border-line bg-panel-2 text-fg hover:border-accent/50 hover:bg-panel-2/80",
  ghost: "text-muted hover:bg-panel-2 hover:text-fg",
  danger: "bg-bad text-white shadow-sm shadow-bad/25 hover:brightness-110",
};

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  busy?: boolean;
  icon?: ReactNode;
}

export function Button({ variant = "secondary", busy = false, icon, children, className = "", disabled, ...rest }: ButtonProps) {
  return (
    <button
      {...rest}
      disabled={disabled || busy}
      className={`inline-flex h-8 items-center gap-1.5 rounded-md px-3 text-[13px] font-medium transition disabled:cursor-not-allowed disabled:opacity-50 ${VARIANTS[variant]} ${className}`}
    >
      {busy ? <LoaderCircle className="size-4 animate-spin" aria-hidden /> : icon}
      {children}
    </button>
  );
}

export function Badge({ className = "", children, title }: { className?: string; children: ReactNode; title?: string }) {
  return (
    <span title={title} className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium ${className}`}>
      {children}
    </span>
  );
}

type Tone = "info" | "warn" | "bad" | "good";

const TONES: Record<Tone, string> = {
  info: "border-line bg-panel-2 text-fg",
  warn: "border-warn/40 bg-warn/10 text-fg",
  bad: "border-bad/40 bg-bad/10 text-fg",
  good: "border-good/40 bg-good/10 text-fg",
};

export function Notice({ tone = "info", icon, children }: { tone?: Tone; icon?: ReactNode; children: ReactNode }) {
  return (
    <div className={`flex items-start gap-2 rounded-lg border px-3 py-2 text-[13px] leading-5 ${TONES[tone]}`}>
      {icon && <span className="mt-0.5 shrink-0">{icon}</span>}
      <div className="min-w-0">{children}</div>
    </div>
  );
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-[12px] font-medium text-muted">{label}</span>
      {children}
      {hint && <span className="text-[12px] text-muted">{hint}</span>}
    </label>
  );
}

export const inputClass =
  "h-8 rounded-md border border-line bg-bg px-2.5 text-[13px] text-fg placeholder:text-muted/70 transition-shadow focus:border-accent focus:ring-2 focus:ring-accent/20 focus:outline-none";

export function PageHeader({ title, description, actions }: { title: string; description?: string; actions?: ReactNode }) {
  return (
    <div className="mb-5 flex items-start justify-between gap-4">
      <div>
        <h1 className="text-xl font-semibold tracking-tight text-fg">{title}</h1>
        {description && <p className="mt-1 max-w-2xl text-[13px] text-muted">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 gap-2">{actions}</div>}
    </div>
  );
}

/** An on/off switch. */
export function Switch({ checked, onChange, label, disabled }: { checked: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
        checked ? "border-accent bg-accent" : "border-line bg-panel-2"
      }`}
    >
      <span className={`inline-block size-3.5 rounded-full bg-white shadow transition-transform ${checked ? "translate-x-[18px]" : "translate-x-[2px]"}`} />
    </button>
  );
}

/** A row of mutually exclusive choices. */
export function Segmented<T extends string | number>({ value, options, onChange, label }: { value: T; options: { value: T; label: string }[]; onChange: (v: T) => void; label: string }) {
  return (
    <div role="radiogroup" aria-label={label} className="inline-flex rounded-md border border-line bg-bg p-0.5">
      {options.map((o) => (
        <button
          key={String(o.value)}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          onClick={() => onChange(o.value)}
          className={`rounded px-2.5 py-1 text-[12px] transition-colors ${value === o.value ? "bg-accent text-accent-fg shadow-sm" : "text-muted hover:text-fg"}`}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
