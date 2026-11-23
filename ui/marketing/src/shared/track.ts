export function track(event: string): void {
  if (typeof window === "undefined") return;
  (window as { dataLayer?: { push: (entry: unknown) => void } }).dataLayer?.push({ event });
}
