import { useId, type CSSProperties } from "react";

export const LIME = "#D8FA7A";
export const DG = "#0B3B2E";
export const EM = "#0A6B52";

// ---------------- Logo: a naira note slides into a coin and closes the G ----------------

export function Mark({ size = 40, tile = DG, anim = false }: { size?: number; tile?: string | null; anim?: boolean }) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 48 48" fill="none" strokeLinecap="round" className="shrink-0">
      {tile && <rect width="48" height="48" rx="13.5" style={{ fill: tile }} />}
      <path d="M34.6 13.4 A15 15 0 1 0 36.3 32.6" stroke="#FFFFFF" strokeWidth="5.5"
        style={anim ? { strokeDasharray: 74, animation: "gvDraw 4s cubic-bezier(.2,.8,.2,1) infinite" } : undefined} />
      <g style={anim ? { animation: "gvNote 4s cubic-bezier(.2,.8,.2,1) infinite" } : undefined}>
        <rect x="21" y="20" width="25" height="9" rx="2.5" fill={LIME} />
        <circle cx="37.5" cy="24.5" r="2" style={{ fill: tile ?? DG }} />
      </g>
    </svg>
  );
}

export function Lockup({ size = 32, anim = false }: { size?: number; anim?: boolean }) {
  return (
    <span className="inline-flex items-center" style={{ gap: size * 0.28 }}>
      <Mark size={size} anim={anim} tile="var(--logo-tile, #0B3B2E)" />
      <span className="font-extrabold tracking-[-0.03em] text-ink" style={{ fontSize: size * 0.62 }}>
        gig<span className="font-medium">vault</span>
      </span>
    </span>
  );
}

// ---------------- Icons: 24px grid, round caps ----------------

const P: Record<string, string> = {
  lock: '<rect x="4" y="10" width="16" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/>',
  home: '<path d="M3 11l9-7 9 7"/><path d="M5 10v10h14V10"/>',
  file: '<path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><path d="M14 3v6h6"/>',
  bank: '<path d="M3 10l9-6 9 6"/><path d="M5 10v8M10 10v8M14 10v8M19 10v8M3 20h18"/>',
  user: '<circle cx="12" cy="8" r="4"/><path d="M4 21c0-4 4-6 8-6s8 2 8 6"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  back: '<path d="M15 18l-6-6 6-6"/>',
  check: '<path d="M5 12l5 5 9-10"/>',
  chev: '<path d="M9 6l6 6-6 6"/>',
  shield: '<path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z"/><path d="M9 12l2 2 4-4"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  alert: '<path d="M12 3l10 18H2z"/><path d="M12 10v5M12 18v.5"/>',
  copy: '<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/>',
  send: '<path d="M22 2L11 13"/><path d="M22 2l-7 20-4-9-9-4z"/>',
  chat: '<path d="M21 12a8 8 0 0 1-12 7l-5 1 1-4a8 8 0 1 1 16-4z"/>',
  cal: '<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4M16 3v4"/>',
  bell: '<path d="M6 16V11a6 6 0 0 1 12 0v5l1.5 2h-15z"/><path d="M10 20a2 2 0 0 0 4 0"/>',
  eye: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
  dots: '<circle cx="5" cy="12" r="1.2"/><circle cx="12" cy="12" r="1.2"/><circle cx="19" cy="12" r="1.2"/>',
  up: '<path d="M7 17L17 7M9 7h8v8"/>',
  down: '<path d="M17 7L7 17M15 17H7V9"/>',
  link: '<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>',
  snow: '<path d="M12 2v20M4 6l16 12M20 6L4 18"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  moon: '<path d="M21 13A9 9 0 1 1 11 3a7 7 0 0 0 10 10z"/>',
  logout: '<path d="M15 4h4a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1h-4"/><path d="M10 17l-5-5 5-5M5 12h11"/>',
  x: '<path d="M6 6l12 12M18 6L6 18"/>',
};
export type IconName = keyof typeof P;

export function Icon({ name, size = 22, sw = 1.7, className, style }: { name: IconName; size?: number; sw?: number; className?: string; style?: CSSProperties }) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={sw}
      strokeLinecap="round" strokeLinejoin="round" className={`shrink-0 ${className ?? ""}`} style={style}
      dangerouslySetInnerHTML={{ __html: P[name] }} />
  );
}

// ---------------- Dither primitives ----------------

function Patterns({ id, color, p }: { id: string; color: string; p: number }) {
  return (
    <defs>
      <pattern id={`${id}12`} width={4 * p} height={4 * p} patternUnits="userSpaceOnUse"><rect width={p} height={p} style={{ fill: color }} /><rect x={2 * p} y={2 * p} width={p} height={p} style={{ fill: color }} /></pattern>
      <pattern id={`${id}25`} width={2 * p} height={2 * p} patternUnits="userSpaceOnUse"><rect width={p} height={p} style={{ fill: color }} /></pattern>
      <pattern id={`${id}50`} width={2 * p} height={2 * p} patternUnits="userSpaceOnUse"><rect width={p} height={p} style={{ fill: color }} /><rect x={p} y={p} width={p} height={p} style={{ fill: color }} /></pattern>
      <pattern id={`${id}75`} width={2 * p} height={2 * p} patternUnits="userSpaceOnUse"><rect width={2 * p} height={p} style={{ fill: color }} /><rect y={p} width={p} height={p} style={{ fill: color }} /></pattern>
    </defs>
  );
}

/** Lime pixel bands fading out, for the edge of dark cards: money going digital. */
export function Strip({ w = 150, h = 180 }: { w?: number; h?: number }) {
  const id = useId().replace(/:/g, "");
  const levels = [12, 25, 50, 75];
  return (
    <svg aria-hidden="true" width={w} height={h} viewBox={`0 0 ${w} ${h}`} shapeRendering="crispEdges"
      className="pointer-events-none absolute right-0 top-0" style={{ opacity: "var(--strip-op)" }}>
      <Patterns id={id} color={LIME} p={4} />
      {levels.map((lv, i) => (
        <rect key={lv} x={(w * (i + 1)) / 5} y={0} width={w / 5 + 0.05} height={h} fill={`url(#${id}${lv})`} />
      ))}
    </svg>
  );
}

/** Round icon tile with a dithered accent dot behind the glyph. */
export function DIcon({ name, size = 44, accent }: { name: IconName; size?: number; accent?: string }) {
  const id = useId().replace(/:/g, "");
  const g = Math.round(size * 0.62);
  return (
    <span className="flex shrink-0 items-center justify-center rounded-full bg-circ" style={{ width: size, height: size }}>
      <svg aria-hidden="true" width={g} height={g} viewBox="0 0 24 24" fill="none">
        <Patterns id={id} color={accent ?? "var(--acc)"} p={1.2} />
        <circle cx="15" cy="15" r="7.5" fill={`url(#${id}50)`} shapeRendering="crispEdges" />
        <g stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" dangerouslySetInnerHTML={{ __html: P[name] }} />
      </svg>
    </span>
  );
}

/** Dither swatch for the design system: solid plus the four pixel densities. */
export function Swatches() {
  const id = useId().replace(/:/g, "");
  return (
    <svg aria-hidden="true" width="200" height="40" viewBox="0 0 50 10" shapeRendering="crispEdges">
      <Patterns id={id} color={LIME} p={1} />
      {[100, 75, 50, 25, 12].map((lv, i) => (
        <g key={lv}><rect x={i * 10} width="10" height="10" fill={DG} /><rect x={i * 10} width="10" height="10" fill={lv === 100 ? LIME : `url(#${id}${lv})`} /></g>
      ))}
    </svg>
  );
}

export function Art({ svg, size, className }: { svg: string; size: number; className?: string }) {
  return <div aria-hidden="true" className={`shrink-0 text-ink ${className ?? ""}`} style={{ width: size, height: size }} dangerouslySetInnerHTML={{ __html: svg }} />;
}

/** Chunky 3D figure, e.g. the "70%" on the home promo. */
export function Tilt({ text, size = 58 }: { text: string; size?: number }) {
  const shadow = "var(--tilt-shadow, #064634)";
  const sh = Array.from({ length: 7 }, (_, i) => `${i + 1}px ${i + 1}px 0 ${shadow}`).join(", ");
  return (
    <div aria-hidden="true" className="font-extrabold leading-none tracking-[-0.05em] text-acc"
      style={{ fontSize: size, textShadow: `${sh}, 10px 14px 18px rgba(0,0,0,0.25)`, animation: "gvFloat 5s ease-in-out infinite", transform: "rotate(-10deg) skewX(-6deg)" }}>
      {text}
    </div>
  );
}

export function Avatar({ name, size = 44, lime = true }: { name: string; size?: number; lime?: boolean }) {
  const ini = name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join("") || "?";
  return (
    <span className={`flex shrink-0 items-center justify-center rounded-full font-extrabold ${lime ? "bg-lime text-dg" : "bg-circ text-ink"}`}
      style={{ width: size, height: size, fontSize: size * 0.34 }}>{ini}</span>
  );
}
