"use client";

import { useEffect, useRef, type CSSProperties, type ReactNode } from "react";
import { cn } from "@/lib/cn";

/**
 * A heading whose letters grow a little bolder near the pointer, falling off smoothly with
 * distance. It uses the variable font's weight axis, so nothing moves or bounces: letters only
 * thicken. With `reveal`, the words also come into focus one after another on load. `adorn`
 * floats a small picture above a word while it is hovered, outside the flow, so no word moves.
 * Screen readers get the plain text; reduced motion turns the effect off.
 */
export function ProximityText({
  text,
  as: Tag = "h2",
  id,
  className,
  reveal = false,
  base = 500,
  peak = 780,
  radius = 150,
  adorn,
}: {
  text: string;
  as?: "h1" | "h2" | "h3";
  id?: string;
  className?: string;
  reveal?: boolean;
  base?: number;
  peak?: number;
  radius?: number;
  /** A picture per word (matched without its punctuation), shown above it on hover. */
  adorn?: Record<string, ReactNode>;
}) {
  const ref = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    if (!window.matchMedia("(hover: hover)").matches) return;
    const letters = Array.from(
      el.querySelectorAll<HTMLSpanElement>("[data-letter]"),
    );
    const weight = letters.map(() => base);
    const target = letters.map(() => base);
    let raf = 0;
    let running = false;

    // Lock each letter's width at the base weight, so a bolder letter thickens inside its own
    // slot and nothing beside it moves. Measured again whenever the heading resizes.
    const lock = () => {
      letters.forEach((l) => {
        l.style.display = "";
        l.style.width = "";
        l.style.fontVariationSettings = `"wght" ${base}`;
      });
      const widths = letters.map((l) => l.getBoundingClientRect().width);
      letters.forEach((l, i) => {
        l.style.display = "inline-block";
        l.style.width = `${widths[i]}px`;
        l.style.textAlign = "center";
        weight[i] = base;
      });
    };
    void document.fonts.ready.then(lock);
    const ro = new ResizeObserver(() => lock());
    ro.observe(el);

    const tick = () => {
      let moving = false;
      letters.forEach((l, i) => {
        const next = weight[i] + (target[i] - weight[i]) * 0.18;
        if (Math.abs(next - weight[i]) > 0.5) moving = true;
        weight[i] = Math.abs(target[i] - next) < 0.5 ? target[i] : next;
        l.style.fontVariationSettings = `"wght" ${weight[i].toFixed(0)}`;
      });
      if (moving) raf = requestAnimationFrame(tick);
      else running = false;
    };
    const kick = () => {
      if (!running) {
        running = true;
        raf = requestAnimationFrame(tick);
      }
    };

    const onMove = (e: PointerEvent) => {
      const box = el.getBoundingClientRect();
      const near =
        e.clientX > box.left - radius &&
        e.clientX < box.right + radius &&
        e.clientY > box.top - radius &&
        e.clientY < box.bottom + radius;
      letters.forEach((l, i) => {
        if (!near) {
          target[i] = base;
          return;
        }
        const r = l.getBoundingClientRect();
        const dx = e.clientX - (r.left + r.width / 2);
        const dy = e.clientY - (r.top + r.height / 2);
        const f = Math.max(0, 1 - Math.sqrt(dx * dx + dy * dy) / radius);
        target[i] = base + (peak - base) * f * f;
      });
      kick();
    };
    const onLeave = () => {
      target.fill(base);
      kick();
    };

    window.addEventListener("pointermove", onMove, { passive: true });
    document.addEventListener("pointerleave", onLeave);
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
      window.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerleave", onLeave);
    };
  }, [base, peak, radius]);

  const words = text.split(" ");
  return (
    <Tag ref={ref} id={id} className={className}>
      <span className="sr-only">{text}</span>
      <span aria-hidden="true">
        {words.map((word, w) => (
          <span key={w}>
            <span
              className={cn(
                "inline-block",
                reveal && "word-in",
                adorn?.[word.replace(/[^\p{L}\p{N}]/gu, "")] &&
                  "group/word relative",
              )}
              style={reveal ? ({ "--i": w } as CSSProperties) : undefined}
            >
              {Array.from(word).map((ch, c) => (
                <span key={c} data-letter="">
                  {ch}
                </span>
              ))}
              {adorn?.[word.replace(/[^\p{L}\p{N}]/gu, "")] ? (
                <span className="pointer-events-none absolute -top-[0.42em] left-1/2 size-[0.5em] -translate-x-1/2 translate-y-[0.18em] -rotate-12 scale-90 opacity-0 transition-[opacity,transform] duration-[420ms] ease-[cubic-bezier(0.22,1,0.36,1)] group-hover/word:translate-y-0 group-hover/word:rotate-0 group-hover/word:scale-100 group-hover/word:opacity-100 motion-reduce:transition-none">
                  {adorn[word.replace(/[^\p{L}\p{N}]/gu, "")]}
                </span>
              ) : null}
            </span>
            {w < words.length - 1 ? " " : null}
          </span>
        ))}
      </span>
    </Tag>
  );
}
