export const STARS_MIN = 50;

type Fetcher = (
  url: string,
  init: { signal: AbortSignal; headers: Record<string, string> },
) => Promise<{
  ok: boolean;
  json: () => Promise<unknown>;
}>;

export async function fetchStars(
  repo: string,
  fetcher: Fetcher = fetch,
  timeoutMs = 3000,
): Promise<number | null> {
  try {
    const res = await fetcher(`https://api.github.com/repos/${repo}`, {
      signal: AbortSignal.timeout(timeoutMs),
      headers: { accept: "application/vnd.github+json" },
    });
    if (!res.ok) return null;
    const count = ((await res.json()) as { stargazers_count?: unknown }).stargazers_count;
    return typeof count === "number" ? count : null;
  } catch {
    return null;
  }
}

export function starsLabel(stars: number | null): string | null {
  if (stars === null || stars < STARS_MIN) return null;
  if (stars < 1000) return String(stars);
  const k = stars / 1000;
  return `${k < 10 ? k.toFixed(1).replace(/\.0$/, "") : Math.round(k)}k`;
}
