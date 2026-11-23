import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vite-plus/test";

const appNames = ["yasaku", "openwa", "gopenwa", "oagent", "othra", "opensheet"];
const exact = ["altalune.id", "Rp"];

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? files(path) : [path];
  });
}

describe("src/shared", () => {
  it("names no app, domain or currency", () => {
    const hits = files(import.meta.dirname)
      .filter((path) => path !== import.meta.filename)
      .flatMap((path) => {
        const text = readFileSync(path, "utf8");
        const lower = text.toLowerCase();
        return [
          ...appNames.filter((name) => lower.includes(name)),
          ...exact.filter((word) => text.includes(word)),
        ].map((word) => `${path}: ${word}`);
      });
    expect(hits).toEqual([]);
  });
});
