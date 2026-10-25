import { MotionConfig } from "motion/react";
import { Benchmarks } from "@/components/sections/benchmarks";
import { Capabilities } from "@/components/sections/capabilities";
import { Closing } from "@/components/sections/closing";
import { Hero } from "@/components/sections/hero";
import { Isolation } from "@/components/sections/isolation";
import { Sdks } from "@/components/sections/sdks";
import { TestedWith } from "@/components/sections/tested-with";
import { Footer } from "@/components/site/footer";
import { Nav } from "@/components/site/nav";

export default function HomePage() {
  return (
    <MotionConfig reducedMotion="user">
      <Nav />
      <main className="flex flex-1 flex-col">
        <Hero />
        <TestedWith />
        <Benchmarks />
        <Capabilities />
        <Isolation />
        <Sdks />
        <Closing />
      </main>
      <Footer />
    </MotionConfig>
  );
}
