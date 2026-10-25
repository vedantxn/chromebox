import Link from "next/link";
import { DotField } from "@/components/fx/dot-field";
import { Logo } from "@/components/logo";
import { batonRepo, repoFile, repoTree, sdk } from "@/lib/shared";
import { StatusDot } from "./status-dot";
import { ButtonLink, GitHubMark } from "./button";
import { Frame } from "./frame";

const columns = [
  {
    title: "Use it",
    links: [
      { href: "/docs", label: "Docs" },
      { href: "/dashboard", label: "Dashboard" },
      { href: sdk.url, label: "batonvm on npm" },
      { href: "/docs#rest", label: "REST API" },
    ],
  },
  {
    title: "Check it",
    links: [
      { href: "/benchmarks", label: "Benchmarks" },
      { href: "/status", label: "Status" },
      { href: "/benchmarks.json", label: "Raw benchmark data" },
      { href: repoTree("docs/learn"), label: "Engineering notes" },
    ],
  },
  {
    title: "Project",
    links: [
      { href: batonRepo.url, label: "GitHub" },
      { href: "https://github.com/vedantxn", label: "Vedant Nayak" },
      { href: repoFile("LICENSE"), label: batonRepo.license },
    ],
  },
];

/**
 * The footer: what Baton is and where to go, then the name itself drawn across the full width
 * as a rust halftone that the page ends on.
 */
export function Footer() {
  return (
    <footer className="mt-auto overflow-hidden border-t border-rule">
      <Frame>
        <div className="grid gap-12 pt-16 pb-12 lg:grid-cols-12">
          <div className="lg:col-span-5">
            <Logo />
            <p className="mt-4 max-w-[34ch] text-[15px] leading-relaxed text-muted-foreground">
              Cloud browsers for AI agents, each in its own Firecracker microVM,
              that move between machines without dropping the connection.
            </p>
            <div className="mt-6 flex flex-wrap items-center gap-3">
              <ButtonLink href={batonRepo.url} variant="secondary" size="sm">
                <GitHubMark className="size-3.5" />
                Star on GitHub
              </ButtonLink>
              <StatusDot />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-8 sm:grid-cols-3 lg:col-span-7">
            {columns.map((c) => (
              <nav key={c.title} aria-label={c.title}>
                <p className="text-[13px] text-subtle">{c.title}</p>
                <ul className="mt-4 space-y-2.5 text-[14px]">
                  {c.links.map((l) => {
                    const cls = "group inline-flex text-foreground";
                    const label = (
                      <span className="link-draw link-draw-quiet">
                        {l.label}
                      </span>
                    );
                    return (
                      <li key={l.label}>
                        {l.href.startsWith("/") ? (
                          <Link href={l.href} className={cls}>
                            {label}
                          </Link>
                        ) : (
                          <a href={l.href} className={cls}>
                            {label}
                          </a>
                        )}
                      </li>
                    );
                  })}
                </ul>
              </nav>
            ))}
          </div>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-rule py-5 text-[13px] text-subtle">
          <span>
            MIT. Built by Vedant Nayak, running on AWS in us-east-1.
          </span>
          <a
            href="#"
            className="group inline-flex items-center gap-1 text-muted-foreground"
          >
            <span className="link-draw">Back to top</span>
            <span
              aria-hidden="true"
              className="transition-transform duration-300 ease-out group-hover:-translate-y-[2px]"
            >
              ↑
            </span>
          </a>
        </div>
      </Frame>
      <div
        aria-hidden="true"
        className="relative mx-auto h-[clamp(96px,19vw,260px)] w-full max-w-[1264px] [mask-image:linear-gradient(to_bottom,#000_55%,transparent)]"
      >
        <DotField word="baton" tint="none" spacing={8} />
      </div>
    </footer>
  );
}
