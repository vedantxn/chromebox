import { DotField } from "@/components/fx/dot-field";
import { AuthForm } from "@/components/site/auth-form";
import { Frame } from "@/components/site/frame";

export const metadata = { title: "Create your account" };

export default function SignUp() {
  return (
    <section className="relative isolate pt-14 pb-24 sm:pt-20 sm:pb-32">
      <div
        aria-hidden="true"
        className="absolute inset-x-0 -top-14 -z-10 h-[520px] [mask-image:radial-gradient(ellipse_60%_70%_at_50%_30%,#000_25%,transparent_72%)]"
      >
        <DotField tint="brand" />
      </div>
      <Frame className="max-w-[460px]">
        <h1 className="display-2 text-center">Create your account</h1>
        <p className="mx-auto mt-3 max-w-[38ch] text-center text-[16px] leading-[1.55] text-muted-foreground">
          Free to start: two browsers at a time, 15 minutes each, 60 browser-minutes a day. No card.
        </p>
        <div className="mt-8">
          <AuthForm mode="sign-up" />
        </div>
      </Frame>
    </section>
  );
}
