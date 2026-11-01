import { cn } from "@/lib/utils";

export function BrandSymbol({ className }: { className?: string }) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 40 40"
      className={cn("shrink-0", className)}
      fill="none"
    >
      <rect width="40" height="40" rx="12" fill="currentColor" />
      <path d="M10 13 20 7.5 30 13 20 18.5 10 13Z" fill="hsl(var(--background))" />
      <path
        d="m10 19 10 5.5L30 19M10 24.5 20 30l10-5.5"
        stroke="hsl(var(--background))"
        strokeWidth="2.7"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
