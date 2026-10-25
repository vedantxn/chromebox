import { Benchmarks } from "@/components/sections/benchmarks";
import { Capabilities } from "@/components/sections/capabilities";
import { Closing } from "@/components/sections/closing";
import { Hero } from "@/components/sections/hero";
import { Isolation } from "@/components/sections/isolation";
import { Sdks } from "@/components/sections/sdks";
import { TestedWith } from "@/components/sections/tested-with";

export default function HomePage() {
  return (
    <>
      <Hero />
      <TestedWith />
      <Benchmarks />
      <Capabilities />
      <Isolation />
      <Sdks />
      <Closing />
    </>
  );
}
