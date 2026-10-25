import Link from "next/link";
import { redirect } from "next/navigation";
import { auth } from "@/auth";
import { Frame } from "@/components/site/frame";
import Live from "./live";

export const metadata = { title: "Session" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const session = await auth();
  if (!session?.uid) redirect("/");
  const { id } = await params;
  return (
    <Frame className="pt-10 pb-24 sm:pt-14 sm:pb-32">
      <Link href="/dashboard" className="group inline-flex items-center gap-1 text-[14px] text-muted-foreground hover:text-foreground">
        <span aria-hidden="true" className="transition-transform duration-300 ease-out group-hover:-translate-x-[3px]">←</span>
        <span className="link-draw link-draw-quiet">Sessions</span>
      </Link>
      <h1 className="mt-3 font-mono text-[26px] font-medium tracking-[-0.02em] sm:text-[30px]">{id}</h1>
      <Live id={id} />
    </Frame>
  );
}

export const instant = false;
