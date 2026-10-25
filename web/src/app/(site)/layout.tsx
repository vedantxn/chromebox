import { MotionConfig } from "motion/react";
import { Footer } from "@/components/site/footer";
import { Nav } from "@/components/site/nav";

export default function SiteLayout({ children }: LayoutProps<"/">) {
  return (
    <MotionConfig reducedMotion="user">
      <Nav />
      <main className="flex flex-1 flex-col">{children}</main>
      <Footer />
    </MotionConfig>
  );
}
