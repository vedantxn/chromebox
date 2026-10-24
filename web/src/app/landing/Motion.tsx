'use client';
import { useEffect } from 'react';

// Entrances: marks the page as scripted, then reveals each [data-reveal]
// group once as it enters (its children rise in turn). Without script, or with
// reduced motion, everything is simply there.
export default function Motion() {
  useEffect(() => {
    const root = document.querySelector('.lp');
    if (!root) return;
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
      root.querySelectorAll('video').forEach((v) => { v.pause(); v.removeAttribute('autoplay'); });
      return;
    }
    if (!('IntersectionObserver' in window)) return;
    root.classList.add('js');
    const els = root.querySelectorAll('[data-reveal], .shutter');
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) {
        if (e.isIntersecting) { e.target.classList.add('is-in'); io.unobserve(e.target); }
      }
    }, { rootMargin: '0px 0px -12% 0px' });
    els.forEach((el) => io.observe(el));
    return () => io.disconnect();
  }, []);
  return null;
}
