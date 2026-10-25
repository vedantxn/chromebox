import "server-only";
import { cacheLife } from "next/cache";
import { batonRepo } from "./shared";

/** The Baton repo's star count, refreshed hourly; null if GitHub doesn't answer. */
export async function starCount(): Promise<number | null> {
  "use cache";
  cacheLife("hours");
  try {
    const res = await fetch(`https://api.github.com/repos/${batonRepo.user}/${batonRepo.repo}`, {
      headers: { Accept: "application/vnd.github+json" },
      signal: AbortSignal.timeout(3000),
    });
    if (!res.ok) return null;
    const body = (await res.json()) as { stargazers_count?: unknown };
    return typeof body.stargazers_count === "number" ? body.stargazers_count : null;
  } catch {
    return null;
  }
}

export { formatStars } from "./format";
