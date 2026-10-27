"use client";

import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { cn } from "@/lib/cn";
import { useLoopClock, usePinnedTime } from "@/lib/use-loop-clock";

/**
 * The hero's product window: one scripted, 14-second session. The agent connects over CDP and
 * shops; halfway through checkout its machine is drained and the browser moves to another one
 * while the agent keeps going; the session ends paused to storage. The create, connect and pause
 * times are real runs' (src/data); the shop is drawn, not a real site.
 */

const LOOP = 14;
const EASE = [0.65, 0, 0.35, 1] as const;

type Tone = "code" | "ok" | "event" | "dim";
type Row = { at: number; text: string; tone: Tone; ms?: number };

// Positions are percentages of the live view.
const TARGETS = {
  start: { x: 82, y: 86 },
  addToCart: { x: 64, y: 68 },
  email: { x: 18, y: 37 },
  place: { x: 28, y: 68 },
};
const CURSOR: { at: number; to: keyof typeof TARGETS }[] = [
  { at: 0, to: "start" },
  { at: 2.3, to: "addToCart" },
  { at: 3.9, to: "email" },
  { at: 7.4, to: "place" },
  { at: 9.6, to: "start" },
];
const CLICKS = [2.95, 4.35, 8.2];
const EMAIL = "agent@acme.dev";
const MOVE = { start: 5.9, end: 6.9 };

function rows(createMs: number, connectMs: number, pausedMs: number, pauseS: string): Row[] {
  return [
    { at: 0.2, text: "baton.sessions.create()", tone: "code" },
    { at: 0.55, text: "session ready", tone: "ok", ms: createMs },
    { at: 0.9, text: "chromium.connectOverCDP(url)", tone: "code" },
    { at: 1.2, text: "connected", tone: "ok", ms: connectMs },
    { at: 1.6, text: 'page.goto("shop.example")', tone: "code" },
    { at: 2.95, text: 'page.click("Add to cart")', tone: "code" },
    { at: 4.35, text: 'page.fill("#email", …)', tone: "code" },
    { at: 5.9, text: "node-a draining", tone: "event" },
    { at: 6.3, text: "moved to node-b, paused", tone: "event", ms: pausedMs },
    { at: 6.9, text: "same connection, 0 dropped", tone: "ok" },
    { at: 8.2, text: 'page.click("Place order")', tone: "code" },
    { at: 10.6, text: "session.pause()", tone: "code" },
    { at: 11.4, text: `stored · no machine holds it · ${pauseS} s`, tone: "dim" },
  ];
}

export function HeroDemo({
  createMs,
  connectMs,
  pausedMs,
  pauseS,
  sessionId,
}: {
  createMs: number;
  connectMs: number;
  pausedMs: number;
  pauseS: string;
  sessionId: string;
}) {
  const reduce = useReducedMotion();
  const pinned = usePinnedTime(LOOP);
  // Reduced motion: hold the frame that explains the most, the browser mid-move.
  const { ref, t } = useLoopClock(LOOP, pinned ?? (reduce ? 6.4 : null));
  const log = rows(createMs, connectMs, pausedMs, pauseS).filter((r) => r.at <= t);

  const live = t >= 1.2 && t < 11.4;
  const page =
    t < 1.6 ? "blank" : t < 4.1 ? "product" : t < 8.6 ? "checkout" : "done";
  const moving = t >= MOVE.start && t < MOVE.end;
  const node = t < 6.3 ? "node-a" : "node-b";
  const stored = t >= 11.4;
  const typed = EMAIL.slice(
    0,
    Math.max(0, Math.min(EMAIL.length, Math.floor((t - 4.45) / 0.055))),
  );
  const cursorTo = ([...CURSOR].reverse().find((c) => c.at <= t) ?? CURSOR[0])
    .to;
  const clicking = CLICKS.some((c) => t >= c && t < c + 0.35);

  return (
    <div
      ref={ref}
      className="rounded-xl bg-stage p-1.5 sm:p-2"
      role="img"
      aria-label="An agent drives a browser in a Baton session. Halfway through checkout the machine under it is drained: the browser moves to another machine, paused for about 225 ms, and the agent carries on over the same connection. The session ends paused to storage."
    >
      <div
        className={cn(
          "overflow-hidden rounded-lg bg-card shadow-float transition-shadow duration-500",
          moving && "shadow-[0_0_0_1.5px_var(--brand),var(--shadow-float)]",
        )}
      >
        {/* Title bar */}
        <div className="flex h-10 items-center gap-3 border-b border-rule bg-muted/60 px-3.5">
          <div className="flex gap-1.5" aria-hidden="true">
            {[0, 1, 2].map((i) => (
              <span key={i} className="size-2.5 rounded-full bg-ink-200" />
            ))}
          </div>
          <div className="mx-auto flex h-6 min-w-0 items-center gap-2 rounded-sm bg-card px-3 font-mono text-[11.5px] text-muted-foreground shadow-sm sm:w-[46%]">
            <span className="size-1.5 shrink-0 rounded-full bg-ink-400" />
            <span className="truncate">
              {page === "blank"
                ? "about:blank"
                : page === "product"
                  ? "shop.example/p/pocket-notebook"
                  : "shop.example/checkout"}
            </span>
          </div>
          <div className="flex items-center gap-3 font-mono text-[11px]">
            <span
              className={cn(
                "flex items-center gap-1.5 transition-opacity",
                live ? "opacity-100" : "opacity-35",
              )}
            >
              <span
                className={cn(
                  "size-1.5 rounded-full",
                  live && !moving ? "animate-pulse bg-success" : moving ? "bg-brand" : "bg-ink-400",
                )}
              />
              {stored ? "PAUSED" : "LIVE"}
            </span>
            <span className="tabular hidden items-center gap-1.5 text-muted-foreground sm:flex">
              <span
                className={cn(
                  "size-1.5 rounded-full",
                  moving ? "bg-brand" : "bg-ink-400",
                )}
              />
              {stored ? "S3" : node}
            </span>
          </div>
        </div>

        <div className="grid md:grid-cols-[minmax(0,38fr)_minmax(0,62fr)]">
          {/* Agent pane */}
          <div className="order-2 border-t border-rule md:order-1 md:border-t-0 md:border-r">
            <div className="flex items-center justify-between border-b border-rule px-4 py-2.5">
              <span className="label-mono text-subtle">agent.ts</span>
              <span
                className={cn(
                  "rounded-xs px-1.5 py-0.5 font-mono text-[10.5px] transition-colors duration-300",
                  moving
                    ? "bg-brand-tint text-brand-ink"
                    : "bg-muted text-muted-foreground",
                )}
              >
                {moving ? "moving to node-b" : stored ? "paused" : `on ${node}`}
              </span>
            </div>
            <ol className="flex h-[188px] flex-col justify-end overflow-hidden px-4 py-3 font-mono text-[12px] leading-[1.75] md:h-[331px] md:justify-start">
              <AnimatePresence initial={false}>
                {log.map((r) => (
                  <motion.li
                    key={r.at}
                    layout="position"
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.25, ease: [0.22, 1, 0.36, 1] }}
                    className="flex items-baseline gap-2 whitespace-nowrap"
                  >
                    <span className="tabular w-9 shrink-0 text-[10.5px] text-subtle">
                      {r.at.toFixed(1)}s
                    </span>
                    <span
                      className={cn(
                        "truncate",
                        r.tone === "code" && "text-foreground",
                        r.tone === "ok" && "text-muted-foreground",
                        r.tone === "dim" && "text-subtle",
                        r.tone === "event" && "text-brand-ink",
                      )}
                    >
                      {r.tone === "code" ? null : (
                        <span aria-hidden="true">
                          {r.tone === "event" ? "● " : "→ "}
                        </span>
                      )}
                      {r.text}
                    </span>
                    {r.ms !== undefined ? (
                      <span className="tabular ml-auto text-foreground">
                        {r.ms} ms
                      </span>
                    ) : null}
                  </motion.li>
                ))}
              </AnimatePresence>
            </ol>
            <div className="hidden items-center justify-between border-t border-rule px-4 py-2.5 font-mono text-[11px] text-subtle md:flex">
              <span className="truncate">{sessionId}</span>
              <span className="flex items-center gap-1.5">
                <span className={cn("size-1.5 rounded-full", moving ? "bg-brand" : "bg-success")} />
                microVM · {stored ? "stored" : node}
              </span>
            </div>
          </div>

          {/* Live view */}
          <div className="relative order-1 aspect-[16/10] overflow-hidden bg-card select-none md:order-2 md:aspect-auto md:h-[412px]">
            <LivePage page={page} typed={typed} cart={t >= 2.95 ? 1 : 0} />

            {/* The move: the page holds still while the machine under it changes */}
            <AnimatePresence>
              {moving || stored ? (
                <motion.div
                  key={stored ? "stored" : "move"}
                  className="absolute inset-0 bg-foreground/[0.05]"
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  exit={{ opacity: 0 }}
                  transition={{ duration: 0.3 }}
                >
                  <motion.div
                    className="absolute top-[34%] left-[24%] w-[52%] rounded-md bg-card p-[clamp(10px,1.8vw,18px)] shadow-float"
                    initial={{ y: 8, opacity: 0 }}
                    animate={{ y: 0, opacity: 1 }}
                    transition={{ duration: 0.35, ease: [0.22, 1, 0.36, 1] }}
                  >
                    {stored ? (
                      <p className="text-[clamp(10px,1.25vw,14px)] font-medium">
                        Paused. Stored, and no machine holds it.
                      </p>
                    ) : (
                      <div className="flex items-center gap-[clamp(6px,1vw,12px)] font-mono text-[clamp(9px,1.1vw,13px)]">
                        <span className={cn("rounded-xs px-1.5 py-0.5", t < 6.3 ? "bg-foreground text-background" : "bg-muted text-subtle")}>node-a</span>
                        <span className="h-px flex-1 bg-rule-strong">
                          <motion.span
                            className="block h-px bg-brand"
                            initial={{ width: "0%" }}
                            animate={{ width: "100%" }}
                            transition={{ duration: 0.45, ease: EASE }}
                          />
                        </span>
                        <span className={cn("rounded-xs px-1.5 py-0.5", t >= 6.3 ? "bg-brand text-brand-foreground" : "bg-muted text-subtle")}>node-b</span>
                      </div>
                    )}
                    <div className="mt-[clamp(10px,1.8vw,18px)] flex items-center justify-between border-t border-rule pt-[clamp(6px,1vw,10px)] font-mono text-[clamp(8px,0.85vw,10px)] text-subtle">
                      <span>{stored ? "session.pause()" : "live migration"}</span>
                      <span className="tabular">{stored ? `${pauseS} s` : `paused ${pausedMs} ms`}</span>
                    </div>
                  </motion.div>
                </motion.div>
              ) : null}
            </AnimatePresence>

            <Cursor
              to={TARGETS[cursorTo]}
              clicking={clicking}
              who="agent"
              hidden={page === "blank" || stored}
            />
          </div>
        </div>
      </div>
    </div>
  );
}

function Cursor({
  to,
  clicking,
  who,
  hidden,
}: {
  to: { x: number; y: number };
  clicking: boolean;
  who: "agent" | "you";
  hidden: boolean;
}) {
  return (
    <motion.div
      aria-hidden="true"
      className="pointer-events-none absolute top-0 left-0 z-10"
      initial={false}
      animate={{
        left: `${to.x}%`,
        top: `${to.y}%`,
        opacity: hidden ? 0 : 1,
      }}
      transition={{ duration: 0.8, ease: EASE }}
    >
      <span className="absolute -top-[9px] -left-[9px] size-[18px]">
        <AnimatePresence>
          {clicking ? (
            <motion.span
              key="ring"
              className={cn(
                "absolute inset-0 rounded-full border-[1.5px]",
                who === "you" ? "border-brand" : "border-foreground",
              )}
              initial={{ scale: 0.2, opacity: 0.6 }}
              animate={{ scale: 1.4, opacity: 0 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.4 }}
            />
          ) : null}
        </AnimatePresence>
      </span>
      <motion.svg
        viewBox="0 0 16 20"
        className="relative -mt-[2px] -ml-[2px] h-[18px] w-[14px] drop-shadow-[0_1px_1px_rgb(0_0_0/0.25)]"
        animate={{ scale: clicking ? 0.88 : 1 }}
        transition={{ duration: 0.09 }}
      >
        <path
          d="M1.5 1.5v14.2l3.7-3.4 2.4 5.6 2.6-1.1-2.4-5.5h5.1z"
          className={cn(
            "transition-colors duration-300",
            who === "you" ? "fill-brand" : "fill-foreground",
          )}
          stroke="white"
          strokeWidth="1.2"
          strokeLinejoin="round"
        />
      </motion.svg>
      <span
        className={cn(
          "absolute top-[18px] left-[10px] rounded-xs px-1.5 py-px font-mono text-[10px] whitespace-nowrap transition-colors duration-300",
          who === "you"
            ? "bg-brand text-brand-foreground"
            : "bg-foreground text-background",
        )}
      >
        {who}
      </span>
    </motion.div>
  );
}

/** A small olive notebook with an elastic band: the shop's product photo. */
function Notebook({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 120 150" className={className} aria-hidden="true">
      <rect x="18" y="12" width="84" height="126" rx="6" fill="#4a5a3f" />
      <rect x="18" y="12" width="10" height="126" rx="3" fill="#3d4b34" />
      <rect
        x="86"
        y="12"
        width="5"
        height="126"
        fill="#23291f"
        opacity="0.85"
      />
      <rect x="42" y="40" width="40" height="14" rx="2" fill="#e9e2cf" />
      <rect x="47" y="45" width="30" height="4" rx="2" fill="#4a5a3f" />
      <rect
        x="20"
        y="136"
        width="80"
        height="4"
        rx="2"
        fill="#000"
        opacity="0.08"
      />
    </svg>
  );
}

const T = "text-[clamp(8px,0.95vw,11.5px)]";

/** The drawn shop, Atlas Paper Co.: a product page, a checkout, then the confirmation. */
function LivePage({
  page,
  typed,
  cart,
}: {
  page: "blank" | "product" | "checkout" | "done";
  typed: string;
  cart: number;
}) {
  return (
    <div className="absolute inset-0" aria-hidden="true">
      {page !== "blank" ? (
        <div className="absolute inset-x-0 top-0 flex h-[11%] items-center gap-[4%] border-b border-rule px-[5%]">
          <span className="font-display text-[clamp(9px,1.15vw,14px)] font-semibold tracking-[-0.02em]">
            Atlas Paper Co.
          </span>
          <span
            className={cn("hidden gap-[10%] text-muted-foreground sm:flex", T)}
          >
            <span>Shop</span>
            <span>Journal</span>
            <span>About</span>
          </span>
          <span
            className={cn(
              "ml-auto flex items-center gap-1.5 text-muted-foreground",
              T,
            )}
          >
            <svg
              viewBox="0 0 16 16"
              className="size-[clamp(10px,1.2vw,14px)]"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.4"
            >
              <path d="M3.5 5.5h9l-.8 7.5H4.3z" strokeLinejoin="round" />
              <path d="M6 5.5V4.5a2 2 0 014 0v1" />
            </svg>
            <span className="tabular">Cart ({cart})</span>
          </span>
        </div>
      ) : null}
      <AnimatePresence mode="wait">
        {page === "product" ? (
          <motion.div
            key="product"
            className="absolute inset-0"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.25 }}
          >
            <div className="absolute top-[18%] left-[6%] grid h-[64%] w-[40%] place-items-center overflow-hidden rounded-md bg-[#efe9df] dark:bg-[#26231f]">
              <Notebook className="h-[78%] -rotate-6 drop-shadow-[0_10px_14px_rgb(60_40_20/0.18)]" />
            </div>
            <div className="absolute top-[18%] left-[52%] w-[42%]">
              <p className={cn("text-subtle", T)}>Notebooks</p>
              <p className="font-display mt-[3%] text-[clamp(11px,1.6vw,19px)] font-medium tracking-[-0.02em]">
                Pocket notebook, olive
              </p>
              <p className="tabular mt-[2%] text-[clamp(9px,1.15vw,14px)]">
                $18.00
              </p>
              <p
                className={cn("mt-[5%] leading-[1.5] text-muted-foreground", T)}
              >
                A6 dot grid, 120 pages of 100gsm paper, lay-flat binding and an
                elastic closure.
              </p>
              <div className="mt-[5%] flex items-center gap-[3%]">
                <span className="aspect-square w-[7%] rounded-full bg-[#4a5a3f] ring-2 ring-foreground ring-offset-2 ring-offset-card" />
                <span className="aspect-square w-[7%] rounded-full bg-[#8a4b2f]" />
                <span className="aspect-square w-[7%] rounded-full bg-[#2f3d52]" />
              </div>
            </div>
            <div
              data-target="addToCart"
              className={cn(
                "absolute top-[63%] left-[52%] flex h-[10%] w-[24%] items-center justify-center rounded-sm bg-foreground font-medium text-background",
                T,
              )}
            >
              Add to cart
            </div>
            <p className={cn("absolute top-[76%] left-[52%] text-subtle", T)}>
              Free shipping over $40
            </p>
          </motion.div>
        ) : page === "checkout" ? (
          <motion.div
            key="checkout"
            className="absolute inset-0"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.25 }}
          >
            <p className="font-display absolute top-[17%] left-[6%] text-[clamp(11px,1.6vw,19px)] font-medium tracking-[-0.02em]">
              Checkout
            </p>
            <Field top={27} label="Email" value={typed} caret />
            <Field top={44} label="Card" value="4242 4242 4242 4242" muted />
            <div
              data-target="place"
              className={cn(
                "absolute top-[63%] left-[6%] flex h-[10%] w-[44%] items-center justify-center rounded-sm bg-foreground font-medium text-background",
                T,
              )}
            >
              Place order · $22.00
            </div>
            <div
              className={cn(
                "absolute top-[17%] left-[56%] w-[38%] rounded-md bg-muted/70 p-[3%]",
                T,
              )}
            >
              <div className="flex items-center gap-[6%]">
                <span className="grid aspect-square w-[22%] place-items-center rounded-sm bg-[#efe9df] dark:bg-[#26231f]">
                  <Notebook className="h-[80%]" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">
                    Pocket notebook
                  </span>
                  <span className="block text-subtle">Olive · 1</span>
                </span>
                <span className="tabular">$18.00</span>
              </div>
              <div className="mt-[8%] space-y-[3%] border-t border-rule pt-[6%] text-muted-foreground">
                <div className="flex justify-between">
                  <span>Shipping</span>
                  <span className="tabular">$4.00</span>
                </div>
                <div className="flex justify-between font-medium text-foreground">
                  <span>Total</span>
                  <span className="tabular">$22.00</span>
                </div>
              </div>
            </div>
          </motion.div>
        ) : page === "done" ? (
          <motion.div
            key="done"
            className="absolute inset-0 flex flex-col items-center justify-center gap-[2.5%]"
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.3 }}
          >
            <span className="grid aspect-square w-[7%] place-items-center rounded-full bg-success/12 text-success">
              <svg viewBox="0 0 16 16" className="w-1/2">
                <path
                  d="M3 8.5l3.2 3L13 4.5"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            </span>
            <p className="font-display text-[clamp(11px,1.6vw,19px)] font-medium tracking-[-0.02em]">
              Thanks, your order is in
            </p>
            <p className={cn("text-muted-foreground", T)}>
              Order #10482 · a receipt is on its way to {EMAIL}
            </p>
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}

function Field({
  top,
  label,
  value,
  caret,
  muted,
}: {
  top: number;
  label: string;
  value: string;
  caret?: boolean;
  muted?: boolean;
}) {
  return (
    <div className="absolute left-[6%] w-[44%]" style={{ top: `${top}%` }}>
      <p className={cn("mb-[2%] text-muted-foreground", T)}>{label}</p>
      <div
        data-target={label.toLowerCase()}
        className="flex h-[clamp(20px,3.4vw,36px)] items-center rounded-sm bg-card px-[4%] text-[clamp(8px,1vw,12.5px)] shadow-sm"
      >
        <span
          className={cn("tabular", muted ? "text-subtle" : "text-foreground")}
        >
          {value}
        </span>
        {caret && value.length > 0 && value.length < EMAIL.length ? (
          <span className="ml-px h-[55%] w-px animate-pulse bg-foreground" />
        ) : null}
      </div>
    </div>
  );
}
