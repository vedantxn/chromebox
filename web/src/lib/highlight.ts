import "server-only";
import { cacheLife } from "next/cache";
import { codeToHtml } from "shiki";
import { codeTheme } from "./code-theme";

/** Code as highlighted HTML, worked out once at build time (shiki reads the clock as it loads). */
export async function highlight(code: string, lang: string) {
  "use cache";
  cacheLife("max");
  return codeToHtml(code, { lang, theme: codeTheme });
}
