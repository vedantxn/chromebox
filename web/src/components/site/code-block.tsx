import { highlight } from "@/lib/highlight";
import { cn } from "@/lib/cn";
import { CopyButton } from "./copy-button";
import { Panel } from "./page";

/** Highlighted code in a panel, with a file name and a copy button. */
export async function CodeBlock({
  code,
  lang = "ts",
  file,
  numbers = true,
}: {
  code: string;
  lang?: string;
  file?: string;
  numbers?: boolean;
}) {
  const html = await highlight(code, lang);
  return (
    <Panel>
      {file ? (
        <div className="flex items-center justify-between border-b border-rule px-4 py-2.5 sm:px-5">
          <span className="font-mono text-[12px] text-subtle">{file}</span>
          <CopyButton value={code} />
        </div>
      ) : null}
      <div className="relative">
        <div
          className={cn("code-block py-4", !file && "pr-10", !numbers && "[&_.line]:pl-5 sm:[&_.line]:pl-6")}
          data-numbers={numbers ? "" : undefined}
          dangerouslySetInnerHTML={{ __html: html }}
        />
        {file ? null : <CopyButton value={code} className="absolute top-3 right-3" />}
      </div>
    </Panel>
  );
}
