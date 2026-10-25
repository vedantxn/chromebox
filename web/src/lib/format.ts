/** 1234 → "1.2k"; small counts as they are. */
export function formatStars(n: number) {
  return n >= 1000
    ? `${(n / 1000).toFixed(1).replace(/\.0$/, "")}k`
    : String(n);
}
