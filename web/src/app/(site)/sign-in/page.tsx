import { DotField } from "@/components/fx/dot-field";
import { AuthForm } from "@/components/site/auth-form";
import { Frame } from "@/components/site/frame";

export const metadata = { title: "Sign in" };

export default function SignIn() {
  return (
    <section className="relative isolate pt-14 pb-24 sm:pt-20 sm:pb-32">
      <div
        aria-hidden="true"
        className="absolute inset-x-0 -top-14 -z-10 h-[520px] [mask-image:radial-gradient(ellipse_60%_70%_at_50%_30%,#000_25%,transparent_72%)]"
      >
        <DotField tint="brand" />
      </div>
      <Frame className="max-w-[460px]">
        <h1 className="display-2 text-center">Sign in</h1>
        <p className="mx-auto mt-3 max-w-[38ch] text-center text-[16px] leading-[1.55] text-muted-foreground">
          Your API keys, your sessions and everything that happened to them.
        </p>
        <div className="mt-8">
          <AuthForm mode="sign-in" />
        </div>
      </Frame>
    </section>
  );
}
