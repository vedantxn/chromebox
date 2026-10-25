import { redirect } from "next/navigation";
import { auth, signOut } from "@/auth";
import { Frame } from "@/components/site/frame";
import Dashboard from "./ui";

export const metadata = { title: "Dashboard" };

export default async function Page() {
  const session = await auth();
  if (!session?.uid) redirect("/");
  return (
    <Frame className="pt-12 pb-24 sm:pt-16 sm:pb-32">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="label-mono text-subtle">Signed in as {session.login}</p>
          <h1 className="display-2 mt-2">Dashboard</h1>
        </div>
        <form
          action={async () => {
            "use server";
            await signOut({ redirectTo: "/" });
          }}
        >
          <button className="rounded-sm px-3 py-1.5 text-[14px] text-muted-foreground transition-colors hover:bg-muted hover:text-foreground">
            Sign out
          </button>
        </form>
      </header>
      <Dashboard />
    </Frame>
  );
}

// Reads the session cookie, so it is rendered per request.
export const instant = false;
