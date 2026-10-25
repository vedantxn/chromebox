"use client";

import { AnimatePresence, motion } from "motion/react";
import Link from "next/link";
import { signIn } from "next-auth/react";
import { useEffect, useState, type FormEvent } from "react";
import { useDeviceSignIn } from "@/app/sign-in";
import { Button, ButtonLink, GitHubMark } from "./button";
import { useSignedIn } from "./get-key";

const field =
  "h-10 w-full rounded-sm bg-card px-3 text-[15px] text-foreground shadow-[inset_0_0_0_1px_var(--rule-strong)] outline-none transition-shadow placeholder:text-subtle focus:shadow-[inset_0_0_0_1.5px_var(--brand)]";

/**
 * Sign in or make an account: email and password, or GitHub. Either way the browser lands on the
 * dashboard, where API keys are made. GitHub uses its device flow: a short code to enter on
 * github.com, then this page goes on by itself.
 */
export function AuthForm({ mode }: { mode: "sign-in" | "sign-up" }) {
  const signedIn = useSignedIn();
  useEffect(() => {
    if (signedIn) location.replace("/dashboard");
  }, [signedIn]);

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const gh = useDeviceSignIn();
  const signup = mode === "sign-up";

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    const res = await signIn("password", { email, password, mode: signup ? "signup" : "signin", redirect: false });
    if (res?.ok && !res.error) {
      location.href = "/dashboard";
      return;
    }
    setError(res?.code && res.code !== "credentials" ? res.code : "Sign-in failed. Try again.");
    setBusy(false);
  };

  return (
    <div className="rounded-lg bg-card p-6 shadow-panel sm:p-8">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <label className="block">
          <span className="text-[13px] font-medium">Email</span>
          <input
            type="email"
            autoComplete="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="you@company.com"
            className={`mt-1.5 ${field}`}
          />
        </label>
        <label className="block">
          <span className="flex items-baseline justify-between text-[13px] font-medium">
            Password
            {signup ? <span className="font-normal text-subtle">8 characters or more</span> : null}
          </span>
          <input
            type="password"
            autoComplete={signup ? "new-password" : "current-password"}
            required
            minLength={signup ? 8 : undefined}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className={`mt-1.5 ${field}`}
          />
        </label>
        {error ? (
          <p role="alert" className="rounded-sm bg-brand-tint px-3 py-2 text-[13.5px] text-brand-ink">
            {error.charAt(0).toUpperCase() + error.slice(1)}
          </p>
        ) : null}
        <Button type="submit" variant="primary" size="lg" disabled={busy || !email || !password} className="w-full">
          {busy ? (signup ? "Creating your account…" : "Signing in…") : signup ? "Create account" : "Sign in"}
        </Button>
      </form>

      <div className="my-6 flex items-center gap-3 text-[12px] text-subtle" aria-hidden="true">
        <span className="h-px flex-1 bg-rule" />
        or
        <span className="h-px flex-1 bg-rule" />
      </div>

      <Button variant="secondary" size="lg" className="w-full" onClick={gh.begin} aria-expanded={Boolean(gh.start)}>
        <GitHubMark />
        Continue with GitHub
      </Button>
      {gh.error ? (
        <p role="alert" className="mt-2 text-[13px] text-brand-ink">
          {gh.error}
        </p>
      ) : null}
      <AnimatePresence>
        {gh.start ? (
          <motion.div
            role="dialog"
            aria-label="Sign in with GitHub"
            initial={{ opacity: 0, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -4 }}
            transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
            className="mt-3 rounded-md bg-muted p-4"
          >
            <p className="label-mono text-subtle">Enter this code on GitHub</p>
            <button type="button" onClick={gh.copy} className="mt-2 font-mono text-[26px] font-semibold tracking-[0.12em]">
              {gh.start.userCode}
            </button>
            <p className="text-[12.5px] text-subtle">{gh.copied ? "Copied." : "Select it to copy."}</p>
            <div className="mt-4 flex items-center justify-between gap-3">
              <ButtonLink href={gh.start.verificationUri} target="_blank" rel="noreferrer" variant="primary" size="sm">
                Open github.com/login/device
              </ButtonLink>
              <button type="button" onClick={gh.cancel} className="text-[13px] text-muted-foreground hover:text-foreground">
                Cancel
              </button>
            </div>
            <p className="mt-3 text-[12.5px] text-subtle">This page goes on by itself once GitHub says yes.</p>
          </motion.div>
        ) : null}
      </AnimatePresence>

      <p className="mt-6 text-center text-[14px] text-muted-foreground">
        {signup ? "Already have an account? " : "New to Baton? "}
        <Link href={signup ? "/sign-in" : "/sign-up"} className="font-medium text-foreground underline decoration-rule-strong underline-offset-[3px] hover:decoration-brand">
          {signup ? "Sign in" : "Create an account"}
        </Link>
      </p>
    </div>
  );
}
