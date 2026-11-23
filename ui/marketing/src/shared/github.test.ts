import { describe, expect, it, vi } from "vite-plus/test";
import { fetchStars, STARS_MIN, starsLabel } from "./github";

const ok = (body: unknown) =>
  vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve(body) });

describe("fetchStars", () => {
  it("reads stargazers_count from the GitHub REST API", async () => {
    const fetcher = ok({ stargazers_count: 120 });
    expect(await fetchStars("owner/repo", fetcher)).toBe(120);
    expect(fetcher).toHaveBeenCalledWith(
      "https://api.github.com/repos/owner/repo",
      expect.objectContaining({ signal: expect.anything() }),
    );
  });

  it("falls back to null on a failed request, a bad status or a bad body", async () => {
    expect(
      await fetchStars("owner/repo", vi.fn().mockRejectedValue(new Error("offline"))),
    ).toBeNull();
    expect(
      await fetchStars(
        "owner/repo",
        vi.fn().mockResolvedValue({ ok: false, json: () => Promise.resolve({}) }),
      ),
    ).toBeNull();
    expect(await fetchStars("owner/repo", ok({ message: "rate limited" }))).toBeNull();
  });

  it("gives up after the timeout", async () => {
    const hang = vi.fn(
      (_url: string, init: { signal: AbortSignal }) =>
        new Promise<never>((_, reject) =>
          init.signal.addEventListener("abort", () => reject(new Error("aborted"))),
        ),
    );
    expect(await fetchStars("owner/repo", hang, 10)).toBeNull();
  });
});

describe("starsLabel", () => {
  it("hides counts below the threshold", () => {
    expect(STARS_MIN).toBe(50);
    expect(starsLabel(null)).toBeNull();
    expect(starsLabel(STARS_MIN - 1)).toBeNull();
    expect(starsLabel(STARS_MIN)).toBe("50");
  });

  it("shortens thousands", () => {
    expect(starsLabel(999)).toBe("999");
    expect(starsLabel(1234)).toBe("1.2k");
    expect(starsLabel(12_000)).toBe("12k");
  });
});
