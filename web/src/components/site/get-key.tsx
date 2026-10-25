"use client";

import { AnimatePresence, motion } from "motion/react";
import { useEffect, useState } from "react";
import { useDeviceSignIn } from "@/app/sign-in";
import { cn } from "@/lib/cn";
import { Button, ButtonLink, GitHubMark } from "./button";

type Size = "sm" | "md" | "lg";

/**
 * The site's one action: sign in with GitHub's device flow and land on the dashboard, where the
 * API key is. The code to enter on GitHub opens in a small panel under the button. A visitor who
 * is already signed in gets a link to the dashboard instead; that is checked in the browser, so
 * the page around it stays static.
 */
export function GetKey({
  size = "lg",
  variant = "primary",
  align = "left",
  inline = false,
}: {
  size?: Size;
  variant?: "primary" | "secondary";
  align?: "left" | "right";
  /** Open the panel in the flow, for a container that clips what overflows it. */
  inline?: boolean;
}) {
  const s = useDeviceSignIn();
  const [signedIn, setSignedIn] = useState(false);
  useEffect(() => {
    fetch("/api/auth/session")
      .then((r) => (r.ok ? r.json() : null))
      .then((j) => setSignedIn(Boolean(j?.user)))
      .catch(() => {});
  }, []);

  if (signedIn) {
    return (
      <ButtonLink href="/dashboard" variant={variant} size={size}>
        Open the dashboard
      </ButtonLink>
    );
  }
  return (
    <div className="relative">
      <Button variant={variant} size={size} onClick={s.begin} aria-expanded={Boolean(s.start)}>
        <GitHubMark />
        Get an API key
      </Button>
      {s.error ? (
        <p role="alert" className="mt-2 text-[13px] text-brand-ink">
          {s.error}
        </p>
      ) : null}
      <AnimatePresence>
        {s.start ? (
          <motion.div
            role="dialog"
            aria-label="Sign in with GitHub"
            initial={{ opacity: 0, y: -4, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -4, scale: 0.98 }}
            transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
            className={cn(
              "mt-2 w-[min(320px,calc(100vw-40px))] rounded-md bg-card p-4 shadow-float",
              !inline && "absolute top-full z-30",
              !inline && (align === "right" ? "right-0" : "left-0"),
            )}
          >
            <p className="label-mono text-subtle">Enter this code on GitHub</p>
            <button
              type="button"
              onClick={s.copy}
              className="mt-2 font-mono text-[26px] font-semibold tracking-[0.12em] text-foreground"
            >
              {s.start.userCode}
            </button>
            <p className="text-[12.5px] text-subtle">
              {s.copied ? "Copied." : "Select it to copy."}
            </p>
            <div className="mt-4 flex items-center justify-between gap-3">
              <ButtonLink
                href={s.start.verificationUri}
                target="_blank"
                rel="noreferrer"
                variant="primary"
                size="sm"
              >
                Open github.com/login/device
              </ButtonLink>
              <button
                type="button"
                onClick={s.cancel}
                className="text-[13px] text-muted-foreground hover:text-foreground"
              >
                Cancel
              </button>
            </div>
            <p className="mt-3 text-[12.5px] text-subtle">
              This page goes on by itself once GitHub says yes.
            </p>
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}
