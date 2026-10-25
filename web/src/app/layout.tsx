import type { Metadata } from "next";
import { ThemeProvider } from "next-themes";
import "./globals.css";

export const metadata: Metadata = {
  metadataBase: new URL("https://batonvm.vercel.app"),
  title: {
    default: "Baton: cloud browsers that move without dropping your agent",
    template: "%s · Baton",
  },
  description:
    "One API call returns a CDP URL to a real Chrome in its own Firecracker microVM. Move it to another machine mid-session, fork it, or pause it to storage, with your agent still connected.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="flex min-h-screen flex-col">
        <ThemeProvider
          attribute="class"
          defaultTheme="light"
          enableSystem={false}
          storageKey="baton-theme"
          disableTransitionOnChange
        >
          {children}
        </ThemeProvider>
      </body>
    </html>
  );
}
