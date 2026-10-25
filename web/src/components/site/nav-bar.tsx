"use client";

import { AnimatePresence, motion } from "motion/react";
import { Menu, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import { Logo } from "@/components/logo";
import { cn } from "@/lib/cn";
import { formatStars } from "@/lib/format";
import { ButtonLink, GitHubMark } from "./button";
import { ThemeToggle } from "./theme-toggle";

export type NavLink = { href: string; label: string };

/**
 * The site header. At the top of the page it is a plain full-width bar; once the page scrolls it
 * gathers into a floating panel (the pattern of Tailark's "Hero Section 1" header, MIT). A
 * highlight slides between links on hover. Below md the links move into a menu.
 */
export function NavBar({
  links,
  repoUrl,
  stars,
}: {
  links: NavLink[];
  repoUrl: string;
  stars: number | null;
}) {
  const [scrolled, setScrolled] = useState(false);
  const [open, setOpen] = useState(false);
  const [hovered, setHovered] = useState<string | null>(null);

  useEffect(() => {
    // Read the position on mount too, so a reload halfway down starts gathered.
    const onScroll = () => setScrolled(window.scrollY > 40);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    const onResize = () => window.innerWidth >= 768 && setOpen(false);
    window.addEventListener("keydown", onKey);
    window.addEventListener("resize", onResize);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("resize", onResize);
    };
  }, [open]);

  return (
    <header className="sticky top-0 z-40 h-14">
      <div
        className={cn(
          "mx-auto flex items-center gap-6 transition-[max-width,margin,padding,height,background-color,box-shadow,border-radius] duration-300 ease-out",
          scrolled || open
            ? "mt-2 h-12 max-w-[min(1000px,calc(100%-24px))] rounded-lg bg-card/85 px-3 shadow-float backdrop-blur-md backdrop-saturate-150 sm:px-4"
            : "h-14 max-w-[1264px] rounded-none bg-transparent px-5 shadow-none sm:px-8",
        )}
      >
        <Link
          href="/"
          aria-label="Baton home"
          className="flex shrink-0 items-center"
          onClick={() => setOpen(false)}
        >
          <Logo />
        </Link>

        <nav
          aria-label="Main"
          className="hidden items-center md:flex"
          onMouseLeave={() => setHovered(null)}
        >
          {links.map((l) => (
            <Link
              key={l.href}
              href={l.href}
              onMouseEnter={() => setHovered(l.href)}
              onFocus={() => setHovered(l.href)}
              onBlur={() => setHovered(null)}
              className={cn(
                "relative rounded-sm px-3 py-1.5 text-[14px] transition-colors duration-150",
                hovered === l.href
                  ? "text-foreground"
                  : "text-muted-foreground",
              )}
            >
              {hovered === l.href ? (
                <motion.span
                  layoutId="nav-hover"
                  className="absolute inset-0 rounded-sm bg-muted"
                  transition={{ type: "spring", stiffness: 500, damping: 38 }}
                />
              ) : null}
              <span className="relative">{l.label}</span>
            </Link>
          ))}
        </nav>

        <div className="ml-auto flex items-center gap-1.5">
          <ThemeToggle />
          <ButtonLink href={repoUrl} variant="primary" size="sm">
            <GitHubMark className="size-3.5" />
            <span className="hidden sm:inline">Star</span>
            {stars ? (
              <span className="tabular font-mono text-[12px] opacity-80">
                {formatStars(stars)}
              </span>
            ) : null}
          </ButtonLink>
          <button
            type="button"
            aria-label={open ? "Close menu" : "Open menu"}
            aria-expanded={open}
            aria-controls="mobile-menu"
            onClick={() => setOpen((o) => !o)}
            className="flex size-8 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground md:hidden"
          >
            {open ? (
              <X className="size-4" strokeWidth={1.75} />
            ) : (
              <Menu className="size-4" strokeWidth={1.75} />
            )}
          </button>
        </div>
      </div>

      <AnimatePresence>
        {open ? (
          <motion.nav
            id="mobile-menu"
            aria-label="Main"
            className="mx-auto mt-2 w-[calc(100%-24px)] rounded-lg bg-popover p-1.5 shadow-float md:hidden"
            initial={{ opacity: 0, y: -6, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -6, scale: 0.98 }}
            transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
          >
            {links.map((l) => (
              <Link
                key={l.href}
                href={l.href}
                onClick={() => setOpen(false)}
                className="block rounded-sm px-3 py-2.5 text-[15px] text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              >
                {l.label}
              </Link>
            ))}
          </motion.nav>
        ) : null}
      </AnimatePresence>
    </header>
  );
}
