// Kupi's mark: a K whose arms open toward a cyan arrow (the pilot's
// heading). Drawn on a 64×64 grid; the K follows the theme (navy on light,
// near-white on dark) and the arrow stays cyan.

export const LOGO_STEM = "M12.5 8h7a2.5 2.5 0 0 1 2.5 2.5V50c0 2.4-1 3.9-3 5.6l-4.4 3.7c-1.6 1.3-4.6.4-4.6-1.9V10.5A2.5 2.5 0 0 1 12.5 8z";
export const LOGO_UPPER_ARM = "M38.6 8h12.2c1 0 1.5 1.2.8 1.9L35.4 26.2a6 6 0 0 1-4.3 1.8h-5.6c-1.1 0-1.8-1.2-1.2-2.2a14 14 0 0 1 2-2.6l9.1-12.2A6 6 0 0 1 38.6 8z";
export const LOGO_LOWER_ARM = "M38.6 56h12.2c1 0 1.5-1.2.8-1.9L35.4 37.8a6 6 0 0 0-4.3-1.8h-5.6c-1.1 0-1.8 1.2-1.2 2.2a14 14 0 0 0 2 2.6l9.1 12.2A6 6 0 0 0 38.6 56z";
export const LOGO_ARROW = "M48.8 23.4 61.8 32 48.8 40.6 51.8 32z";

export function KupiLogo({ className = "size-7", title = "Kupi" }: { className?: string; title?: string }) {
  return (
    <svg viewBox="0 0 64 64" className={className} role="img" aria-label={title}>
      <g fill="var(--kupi-logo-k)">
        <path d={LOGO_STEM} />
        <path d={LOGO_UPPER_ARM} />
        <path d={LOGO_LOWER_ARM} />
      </g>
      <path d={LOGO_ARROW} fill="var(--kupi-logo-arrow)" stroke="var(--kupi-logo-arrow)" strokeWidth={1.8} strokeLinejoin="round" />
    </svg>
  );
}

/** Logo plus the wordmark, as in the brand image. */
export function KupiWordmark({ className = "" }: { className?: string }) {
  return (
    <span className={`inline-flex items-center gap-2 ${className}`}>
      <KupiLogo className="size-7" />
      <span className="text-[15px] font-semibold tracking-[0.22em] text-fg">KUPI</span>
    </span>
  );
}
