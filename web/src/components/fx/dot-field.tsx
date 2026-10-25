"use client";

import { useEffect, useRef } from "react";
import { MARK_PATHS, MARK_VIEWBOX } from "@/components/logo";
import { cn } from "@/lib/cn";

/**
 * A field of dots on a canvas. A slow swell of light drifts across it, and dots near the pointer
 * grow and warm to the brand colour. With `wing` the Baton mark, or with `word` a word, is
 * drawn into the field as a brand-coloured halftone. It draws only while on screen and the tab
 * is visible; under reduced motion it draws one still frame.
 */
export function DotField({
  className,
  spacing = 18,
  tint = "ink",
  wing,
  word,
}: {
  className?: string;
  spacing?: number;
  /**
   * The dots outside the shape. "brand": rust, always on, a little bolder near the pointer;
   * "none": only the shape's dots are drawn.
   */
  tint?: "ink" | "brand" | "none";
  /** Draw the wing mark into the field: where it sits and how tall it is (share of height). */
  wing?: {
    align: "center" | "right";
    height: number;
    /** Share of the mark that sits below the bottom edge, so it rises from it. */
    rise?: number;
  };
  /** Draw a word across the full width, sitting on the bottom edge (a footer wordmark). */
  word?: string;
}) {
  const ref = useRef<HTMLCanvasElement>(null);
  const wingAlign = wing?.align;
  const wingHeight = wing?.height;
  const wingRise = wing?.rise;

  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const reduce = window.matchMedia(
      "(prefers-reduced-motion: reduce)",
    ).matches;

    let w = 0;
    let h = 0;
    let dpr = 1;
    let dots: { x: number; y: number; inWing: number }[] = [];
    let ink = "#a8a29e";
    let brand = "#b4441c";
    const pointer = { x: -9999, y: -9999, k: 0 };
    let visible = false;
    let raf = 0;

    const readColours = () => {
      const cs = getComputedStyle(document.documentElement);
      ink = cs.getPropertyValue("--ink-400").trim() || ink;
      brand = cs.getPropertyValue("--brand").trim() || brand;
    };

    // Which dots fall inside the shape: draw it on an offscreen canvas and sample it.
    const wordMask = () => {
      if (!word) return null;
      const off = document.createElement("canvas");
      off.width = Math.ceil(w);
      off.height = Math.ceil(h);
      const o = off.getContext("2d")!;
      const family = '"Host Grotesk Variable", ui-sans-serif, sans-serif';
      o.font = `600 100px ${family}`;
      const fit = (100 * w * 0.97) / o.measureText(word).width;
      const size = Math.min(fit, h * 1.25);
      o.font = `600 ${size}px ${family}`;
      o.textAlign = "center";
      o.textBaseline = "alphabetic";
      o.fillText(word, w / 2, h * 0.96);
      return o.getImageData(0, 0, off.width, off.height);
    };
    const wingMask = () => {
      if (word) return wordMask();
      if (!wingAlign || !wingHeight) return null;
      const [, , vw, vh] = MARK_VIEWBOX.split(" ").map(Number);
      const mh = h * wingHeight;
      const mw = (mh * vw) / vh;
      const ox =
        wingAlign === "right" && w > 900 ? w - mw - w * 0.08 : (w - mw) / 2;
      const oy =
        wingRise !== undefined ? h - mh * (1 - wingRise) : (h - mh) / 2;
      const off = document.createElement("canvas");
      off.width = Math.ceil(w);
      off.height = Math.ceil(h);
      const o = off.getContext("2d")!;
      o.translate(ox, oy - (9 * mh) / vh);
      o.scale(mh / vh, mh / vh);
      for (const p of MARK_PATHS) {
        o.globalAlpha = p.opacity;
        o.fill(new Path2D(p.d));
      }
      return o.getImageData(0, 0, off.width, off.height);
    };

    const layout = () => {
      const r = canvas.getBoundingClientRect();
      w = r.width;
      h = r.height;
      dpr = Math.min(window.devicePixelRatio || 1, 2);
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
      const mask = wingMask();
      dots = [];
      const off = spacing / 2;
      for (let y = off; y < h; y += spacing) {
        for (let x = off; x < w; x += spacing) {
          let inWing = 0;
          if (mask) {
            const i = (Math.round(y) * mask.width + Math.round(x)) * 4 + 3;
            inWing = (mask.data[i] ?? 0) / 255;
          }
          dots.push({ x, y, inWing });
        }
      }
    };

    const draw = (time: number) => {
      const t = time / 1000;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);
      // Ease the pointer's influence in and out instead of snapping.
      pointer.k += ((pointer.x > -999 ? 1 : 0) - pointer.k) * 0.08;
      const R = 150;
      for (const d of dots) {
        const swell = 0.5 + 0.5 * Math.sin(d.x * 0.011 + d.y * 0.007 - t * 0.7);
        const dx = d.x - pointer.x;
        const dy = d.y - pointer.y;
        const near = Math.max(0, 1 - Math.sqrt(dx * dx + dy * dy) / R);
        const heat = near * near * pointer.k;
        if (d.inWing > 0.05) {
          // A halftone of the mark: its three layers (100%, 60%, 30%) as three dot sizes.
          ctx.fillStyle = brand;
          ctx.globalAlpha = Math.min(
            1,
            0.25 + d.inWing * (0.55 + 0.2 * swell) + heat * 0.3,
          );
          const r = 0.5 + d.inWing * (1.9 + 0.35 * swell) + heat * 0.8;
          ctx.beginPath();
          ctx.arc(d.x, d.y, r, 0, Math.PI * 2);
          ctx.fill();
          continue;
        }
        if (tint === "none") continue;
        if (tint === "brand") {
          ctx.fillStyle = brand;
          ctx.globalAlpha = 0.33 + 0.11 * swell + heat * 0.18;
          const r = 1.2 + 0.3 * swell + heat * 0.5;
          ctx.beginPath();
          ctx.arc(d.x, d.y, r, 0, Math.PI * 2);
          ctx.fill();
          continue;
        }
        ctx.fillStyle = heat > 0.02 ? brand : ink;
        ctx.globalAlpha = 0.16 + 0.22 * swell * swell + heat * 0.6;
        const r = 0.85 + heat * 1.4;
        ctx.beginPath();
        ctx.arc(d.x, d.y, r, 0, Math.PI * 2);
        ctx.fill();
      }
      ctx.globalAlpha = 1;
    };

    const loop = (time: number) => {
      if (visible && document.visibilityState === "visible") draw(time);
      raf = requestAnimationFrame(loop);
    };

    const onMove = (e: PointerEvent) => {
      const r = canvas.getBoundingClientRect();
      const inside =
        e.clientX >= r.left &&
        e.clientX <= r.right &&
        e.clientY >= r.top &&
        e.clientY <= r.bottom;
      pointer.x = inside ? e.clientX - r.left : -9999;
      pointer.y = inside ? e.clientY - r.top : -9999;
    };

    readColours();
    layout();
    // The word is drawn in the heading font: lay out again once it has loaded.
    if (word) {
      void document.fonts.load('600 100px "Host Grotesk Variable"').then(() => {
        layout();
        if (reduce) draw(0);
      });
    }
    const ro = new ResizeObserver(() => {
      layout();
      if (reduce) draw(0);
    });
    ro.observe(canvas);
    // Re-read the colours when the theme changes.
    const mo = new MutationObserver(() => {
      readColours();
      if (reduce) draw(0);
    });
    mo.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "style"],
    });

    if (reduce) {
      draw(0);
      return () => {
        ro.disconnect();
        mo.disconnect();
      };
    }

    const io = new IntersectionObserver(([e]) => (visible = e.isIntersecting));
    io.observe(canvas);
    window.addEventListener("pointermove", onMove, { passive: true });
    raf = requestAnimationFrame(loop);
    return () => {
      cancelAnimationFrame(raf);
      io.disconnect();
      ro.disconnect();
      mo.disconnect();
      window.removeEventListener("pointermove", onMove);
    };
  }, [spacing, tint, wingAlign, wingHeight, wingRise, word]);

  return (
    <canvas
      ref={ref}
      aria-hidden="true"
      className={cn(
        "pointer-events-none absolute inset-0 size-full",
        className,
      )}
    />
  );
}
