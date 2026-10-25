import { starCount } from "@/lib/github";
import { batonRepo } from "@/lib/shared";
import { NavBar } from "./nav-bar";

const links = [
  { href: "/#benchmarks", label: "Benchmarks" },
  { href: "/#capabilities", label: "Features" },
  { href: "/#isolation", label: "How it works" },
  { href: "/docs", label: "Docs" },
  { href: "/status", label: "Status" },
];

export async function Nav() {
  const stars = await starCount();
  return <NavBar links={links} repoUrl={batonRepo.url} stars={stars} />;
}
