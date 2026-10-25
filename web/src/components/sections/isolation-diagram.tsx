"use client";

import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { cn } from "@/lib/cn";
import { useLoopClock, usePinnedTime } from "@/lib/use-loop-clock";

/**
 * How a session moves. CDP comes in through the gateway, which routes the VM's address to the
 * machine that holds it. Mid-loop the VM moves from node-a to node-b: only the route changes, so
 * the agent's connection carries on. What a page sends out passes each node's firewall, which
 * lets example.com through and refuses the cloud metadata address.
 */

const LOOP = 7.2;
type Pt = [number, number];
const MOVE = 3.2;

const ROUTES: Record<string, Pt[]> = {
  cdpA: [
    [144, 190],
    [338, 190],
    [338, 96],
    [368, 96],
  ],
  cdpB: [
    [144, 190],
    [338, 190],
    [338, 284],
    [368, 284],
  ],
  allowed: [
    [532, 96],
    [566, 96],
    [566, 190],
    [728, 190],
    [748, 190],
    [748, 118],
    [768, 118],
  ],
  denied: [
    [532, 284],
    [566, 284],
    [566, 190],
    [632, 190],
  ],
};

const WIRES: Pt[][] = [
  [
    [144, 190],
    [188, 190],
  ],
  [
    [308, 190],
    [338, 190],
    [338, 96],
    [368, 96],
  ],
  [
    [308, 190],
    [338, 190],
    [338, 284],
    [368, 284],
  ],
  [
    [532, 96],
    [566, 96],
    [566, 190],
    [600, 190],
  ],
  [
    [532, 284],
    [566, 284],
    [566, 190],
  ],
  [
    [728, 190],
    [748, 190],
    [748, 118],
    [768, 118],
  ],
];
const DENIED_WIRE: Pt[] = [
  [728, 190],
  [748, 190],
  [748, 262],
  [768, 262],
];

function along(route: Pt[], f: number): Pt {
  const segs = route.slice(1).map((p, i) => {
    const q = route[i];
    return { from: q, to: p, len: Math.hypot(p[0] - q[0], p[1] - q[1]) };
  });
  let d = Math.max(0, Math.min(1, f)) * segs.reduce((s, x) => s + x.len, 0);
  for (const s of segs) {
    if (d <= s.len) {
      const k = s.len ? d / s.len : 0;
      return [
        s.from[0] + (s.to[0] - s.from[0]) * k,
        s.from[1] + (s.to[1] - s.from[1]) * k,
      ];
    }
    d -= s.len;
  }
  return route[route.length - 1];
}

const ease = (x: number) =>
  x < 0.5 ? 4 * x * x * x : 1 - Math.pow(-2 * x + 2, 3) / 2;
const phase = (t: number, a: number, b: number) =>
  t < a ? -1 : t > b ? 2 : ease((t - a) / (b - a));

const d = (pts: Pt[]) =>
  pts.map((p, i) => `${i ? "L" : "M"}${p[0]} ${p[1]}`).join(" ");

function Box({
  x,
  y,
  w,
  h,
  title,
  sub,
  tone = "plain",
}: {
  x: number;
  y: number;
  w: number;
  h: number;
  title: string;
  sub?: string;
  tone?: "plain" | "focal" | "denied";
}) {
  return (
    <g>
      <rect
        x={x}
        y={y}
        width={w}
        height={h}
        rx={8}
        className={cn(
          "fill-card transition-colors duration-300",
          tone === "focal" && "stroke-ink-800",
          tone === "plain" && "stroke-ink-400/70",
          tone === "denied" && "stroke-brand",
        )}
        strokeWidth={tone === "plain" ? 1 : 1.25}
      />
      <text
        x={x + 12}
        y={y + (sub ? h / 2 - 3 : h / 2 + 4)}
        className="fill-foreground text-[12.5px] font-medium"
      >
        {title}
      </text>
      {sub ? (
        <text
          x={x + 12}
          y={y + h / 2 + 13}
          className="fill-subtle font-mono text-[10px]"
        >
          {sub}
        </text>
      ) : null}
    </g>
  );
}

export function IsolationDiagram({ pausedMs }: { pausedMs: number }) {
  const reduce = useReducedMotion();
  const pinned = usePinnedTime(LOOP);
  const { ref, t } = useLoopClock(LOOP, pinned ?? (reduce ? 3.5 : null));

  const onB = t >= MOVE + 0.3;
  const moving = t >= MOVE && t < MOVE + 0.7;
  const cdpA = phase(t, 0.2, 1.3);
  const allowed = phase(t, 1.2, 2.9);
  const cdpB = phase(t, 4.0, 5.1);
  const denied = phase(t, 5.1, 5.9);
  const blocked = t >= 5.9 && t < 7.0;
  const reached = t >= 2.9 && t < 3.6;

  const packet = (key: string, f: number, route: Pt[], red = false) => {
    if (f < 0 || f > 1) return null;
    const [x, y] = along(route, f);
    return (
      <rect
        key={key}
        x={x - 6}
        y={y - 3.5}
        width={12}
        height={7}
        rx={3.5}
        className={red ? "fill-brand" : "fill-ink-800"}
      />
    );
  };

  return (
    <div ref={ref} className="relative">
      <svg
        viewBox="0 0 900 380"
        className="h-auto w-full"
        role="img"
        aria-label="Your agent connects through the gateway, which routes to the machine holding the browser's microVM. The microVM moves from node-a to node-b and only the route changes. What the page sends out passes the node's firewall: example.com is allowed, 169.254.169.254, the cloud metadata address, is refused."
      >
        {/* Two machines; a VM's address goes with it */}
        {[
          { y: 24, name: "node-a" },
          { y: 212, name: "node-b" },
        ].map((n) => (
          <g key={n.name}>
            <rect
              x={352}
              y={n.y}
              width={196}
              height={144}
              rx={14}
              className="fill-muted/60 stroke-ink-400"
              strokeDasharray="4 4"
            />
            <text x={366} y={n.y + 20} className="fill-subtle text-[11px]">
              {n.name}, jailed Firecracker
            </text>
          </g>
        ))}

        {WIRES.map((w, i) => (
          <path
            key={i}
            d={d(w)}
            fill="none"
            className={cn(
              "transition-colors duration-300",
              (i === 1 && !onB) || (i === 2 && onB) ? "stroke-ink-800" : "stroke-ink-400/80",
            )}
            strokeWidth={(i === 1 && !onB) || (i === 2 && onB) ? 1.25 : 1}
          />
        ))}
        <path
          d={d(DENIED_WIRE)}
          fill="none"
          className={cn(
            "transition-colors duration-300",
            blocked ? "stroke-brand" : "stroke-ink-400/80",
          )}
          strokeWidth={1}
          strokeDasharray="3 4"
        />

        <Box x={16} y={160} w={128} h={60} title="Your agent" sub="any CDP client" />
        <Box x={188} y={160} w={120} h={60} title="Gateway" sub="CDP proxy" tone={moving ? "denied" : "plain"} />
        <Box
          x={368}
          y={64}
          w={164}
          h={64}
          title={onB ? "free" : "microVM"}
          sub={onB ? "room for the next" : "Chrome · vm-7f3a21"}
          tone={onB ? "plain" : "focal"}
        />
        <Box
          x={368}
          y={252}
          w={164}
          h={64}
          title={onB ? "microVM" : "free"}
          sub={onB ? "Chrome · vm-7f3a21" : "room for the next"}
          tone={onB ? "focal" : "plain"}
        />
        <Box
          x={600}
          y={160}
          w={128}
          h={60}
          title="Firewall"
          sub="nftables"
          tone={blocked ? "denied" : "focal"}
        />
        <Box
          x={768}
          y={90}
          w={124}
          h={56}
          title="example.com"
          sub="allowed"
          tone={reached ? "focal" : "plain"}
        />
        <Box
          x={768}
          y={234}
          w={124}
          h={56}
          title="169.254.169.254"
          sub="cloud metadata"
          tone={blocked ? "denied" : "plain"}
        />

        {packet("cdpA", cdpA, ROUTES.cdpA)}
        {packet("allowed", allowed, ROUTES.allowed)}
        {packet("cdpB", cdpB, ROUTES.cdpB)}
        {packet(
          "denied",
          denied === 2 && blocked ? 1 : denied,
          ROUTES.denied,
          blocked,
        )}
      </svg>

      <AnimatePresence>
        {moving ? (
          <motion.div
            key="move"
            className="absolute top-[45%] left-[37%] rounded-xs bg-card px-2 py-1 font-mono text-[11px] whitespace-nowrap text-brand-ink shadow-sm"
            initial={{ opacity: 0, scale: 0.96, y: 4 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18 }}
          >
            node-a → node-b · paused {pausedMs} ms
          </motion.div>
        ) : null}
        {blocked ? (
          <motion.div
            key="chip"
            className="absolute top-[60%] left-[66.5%] rounded-xs bg-card px-2 py-1 font-mono text-[11px] whitespace-nowrap text-brand-ink shadow-sm"
            initial={{ opacity: 0, scale: 0.96, y: 4 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18 }}
          >
            dropped · blocked range
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}
