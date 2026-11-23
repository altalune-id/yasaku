import { describe, expect, it } from "vite-plus/test";
import { site } from "../site";
import { copy } from ".";

const typed = {
  en: ["", "", ""],
  id: ["", "", ""],
};

const ctas = {
  en: { primary: "Start free", link: "Copy link", prompt: "Copy prompt", copied: "Copied ✓" },
  id: {
    primary: "Mulai gratis",
    link: "Salin tautan",
    prompt: "Salin prompt",
    copied: "Tersalin ✓",
  },
};

describe.each(["en", "id"] as const)("copy %s", (locale) => {
  const c = copy[locale];

  it("fits the head limits", () => {
    expect(c.meta.title.length).toBeLessThanOrEqual(60);
    expect(c.meta.description.length).toBeLessThanOrEqual(155);
  });

  it("has 7 FAQ items and 6 feature cards", () => {
    expect(c.faq.items).toHaveLength(7);
    expect(c.features.cards).toHaveLength(6);
  });

  it("uses the pm CTA labels", () => {
    for (const label of [c.hero.primaryCta, c.nav.cta, c.finalCta.cta, c.openSource.cloud.cta]) {
      expect(label).toBe(ctas[locale].primary);
    }
    expect(c.ai.setup.chat.copy).toBe(ctas[locale].link);
    expect(c.ai.setup.coding.copy).toBe(ctas[locale].prompt);
    expect(c.copyBar.copied).toBe(ctas[locale].copied);
  });

  it("leads the hero with the product and names AI only in the sub line", () => {
    for (const text of [c.hero.eyebrow, c.hero.title, c.hero.primaryCta]) {
      expect(text).not.toMatch(/\b(?:AI|MCP|ChatGPT|Claude)\b/);
    }
    expect(c.hero.sub).not.toMatch(/\b(?:MCP|ChatGPT|Claude|Cursor)\b/);
    expect(c.hero.sub).toMatch(/\bAI\b/);
  });

  it("keeps MCP out of every headline", () => {
    const headlines = [
      c.hero.title,
      c.features.title,
      c.payday.title,
      c.ai.title,
      c.ai.stepsTitle,
      c.openSource.title,
      c.faq.title,
      c.finalCta.title,
    ];
    for (const text of headlines) expect(text).not.toMatch(/\bMCP\b/);
  });

  it("spells the brand Yasaku outside URLs and paths", () => {
    const strings = (value: unknown): string[] =>
      typeof value === "string"
        ? [value]
        : value && typeof value === "object"
          ? Object.values(value).flatMap(strings)
          : [];
    const lowercase = strings(c).filter((text) => /(?<![\w/.@-])yasaku(?![\w.-]|:\/)/.test(text));
    expect(lowercase).toEqual([]);
    expect(c.footer.copyright).toBe("\u00a9 {year} altalune.id");
  });

  it("says MCP only in the copy bar and the FAQ", () => {
    const strings = (value: unknown): string[] =>
      typeof value === "string"
        ? [value]
        : value && typeof value === "object"
          ? Object.values(value).flatMap(strings)
          : [];
    const { copyBar: _copyBar, faq: _faq, ai, ...others } = c;
    const rest = { ...others, ai };
    expect(strings(rest).filter((text) => /\bMCP\b/.test(text))).toEqual([]);
  });

  it("offers a copy prompt that carries the MCP URL", () => {
    expect(c.copyBar.value).toContain(site.mcpUrl);
    expect(c.copyBar.value).toMatch(/Yasaku/);
  });

  it("keeps the technical FAQ answer to two sentences", () => {
    const answer = c.faq.items.find((i) => /MCP/.test(i.a))!.a;
    expect(answer).toMatch(/OAuth/);
    expect(answer.split(/(?<=\.)\s+/).length).toBeLessThanOrEqual(2);
  });

  it("names assistants only in the rotator and the trademark note", () => {
    const strings = (value: unknown): string[] =>
      typeof value === "string"
        ? [value]
        : value && typeof value === "object"
          ? Object.values(value).flatMap(strings)
          : [];
    const { ai, footer, ...rest } = c;
    const { rotatorNames: _names, ...aiRest } = ai;
    const { trademarks: _tm, ...footerRest } = footer;
    const named = strings({ ...rest, ai: aiRest, footer: footerRest }).filter((t) =>
      /ChatGPT|Claude|Cursor|Gemini/.test(t),
    );
    expect(named).toEqual([]);
    expect(c.footer.trademarks).not.toMatch(/Cursor/);
  });

  it("makes no privacy claim the product cannot back yet", () => {
    expect(JSON.stringify(c)).not.toMatch(/never sell|tidak pernah menjual/i);
  });

  it("puts the free, no-card reassurance at the final button", () => {
    expect(c.finalCta.note).toMatch(locale === "en" ? /Free\. No card/ : /Gratis\. Tanpa kartu/);
  });

  it("rotates tested assistants and rests on your AI", () => {
    expect(c.ai.rotatorNames.at(-1)).toBe(locale === "en" ? "your AI" : "asisten AI Anda");
    expect(c.ai.rotatorNames).toEqual([
      "ChatGPT",
      "Claude",
      locale === "en" ? "your AI" : "asisten AI Anda",
    ]);
    expect(c.ai.title).not.toMatch(/ChatGPT|Claude|Cursor/);
  });

  it("never tells visitors to paste a link that is no longer shown", () => {
    for (const step of c.ai.steps) {
      expect(`${step.body} ${step.typed}`).not.toMatch(/\blink\b|\btautan\b|https?:\/\//i);
    }
    expect(c.ai.steps[0]!.typed).toBe("");
  });

  it("never shows a bare quoted example line in a step", () => {
    for (const step of c.ai.steps) expect(step.body).not.toMatch(/^["\u201c]/);
  });

  it("types the agreed strings in the AI steps", () => {
    expect(c.ai.steps.map((s) => s.typed)).toEqual(typed[locale]);
  });

  it("lists the self-host installs with no secret", () => {
    const text = JSON.stringify(c.openSource.selfHost);
    expect(text).toContain("ghcr.io/altalune-id/yasaku");
    expect(c.openSource.selfHost.items.map((i) => i.detailHref)).toContain(
      `${site.githubUrl}/releases`,
    );
    expect(text).not.toMatch(/docker run|change-me|PASSWORD|SECRET|KEY=/i);
  });
});
