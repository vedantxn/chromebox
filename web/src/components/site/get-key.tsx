"use client";

import { useEffect, useState } from "react";
import { ButtonLink } from "./button";

type Size = "sm" | "md" | "lg";

/** Whether this browser is signed in; checked in the browser, so pages around it stay static. */
export function useSignedIn() {
  const [signedIn, setSignedIn] = useState(false);
  useEffect(() => {
    fetch("/api/auth/session")
      .then((r) => (r.ok ? r.json() : null))
      .then((j) => setSignedIn(Boolean(j?.user)))
      .catch(() => {});
  }, []);
  return signedIn;
}

/**
 * The site's one action: make an account, which is where the API key comes from. A visitor who
 * is already signed in gets a link to the dashboard instead.
 */
export function GetKey({
  size = "lg",
  variant = "primary",
}: {
  size?: Size;
  variant?: "primary" | "secondary";
}) {
  const signedIn = useSignedIn();
  return signedIn ? (
    <ButtonLink href="/dashboard" variant={variant} size={size}>
      Open the dashboard
    </ButtonLink>
  ) : (
    <ButtonLink href="/sign-up" variant={variant} size={size}>
      Get an API key
    </ButtonLink>
  );
}

/** Sign in, or the dashboard once signed in: a small link for the nav. */
export function AccountLink() {
  const signedIn = useSignedIn();
  return (
    <ButtonLink href={signedIn ? "/dashboard" : "/sign-in"} variant="ghost" size="sm">
      {signedIn ? "Dashboard" : "Sign in"}
    </ButtonLink>
  );
}
