"use client";

import { useEffect, useRef, useState } from "react";

/**
 * Seconds into a `loop`-second cycle, ticking only while the element is on screen and the tab is
 * visible. `frozenAt` holds one moment instead (reduced motion, or ?t= for screenshots).
 */
export function useLoopClock<T extends Element = HTMLDivElement>(
  loop: number,
  frozenAt: number | null,
) {
  const ref = useRef<T>(null);
  const [running, setT] = useState(0);
  useEffect(() => {
    if (frozenAt !== null) return;
    const el = ref.current;
    if (!el) return;
    let visible = false;
    let last = performance.now();
    let acc = 0;
    let raf = 0;
    const tick = (now: number) => {
      // A frame's timestamp can be a little earlier than the performance.now() we started from.
      const dt = Math.max(0, Math.min((now - last) / 1000, 0.1));
      last = now;
      if (visible && document.visibilityState === "visible") {
        acc = (acc + dt) % loop;
        setT(acc);
      }
      raf = requestAnimationFrame(tick);
    };
    const io = new IntersectionObserver(
      ([e]) => {
        visible = e.isIntersecting;
        last = performance.now();
      },
      { threshold: 0.2 },
    );
    io.observe(el);
    raf = requestAnimationFrame(tick);
    return () => {
      io.disconnect();
      cancelAnimationFrame(raf);
    };
  }, [frozenAt, loop]);
  return { ref, t: frozenAt ?? running };
}

/** ?t=7.3 in the URL: the moment to hold, for screenshots. */
export function usePinnedTime(loop: number) {
  const [pinned, setPinned] = useState<number | null>(null);
  useEffect(() => {
    const v = Number(new URLSearchParams(location.search).get("t"));
    // eslint-disable-next-line react-hooks/set-state-in-effect -- the URL is only known on the client
    if (v > 0) setPinned(v % loop);
  }, [loop]);
  return pinned;
}
