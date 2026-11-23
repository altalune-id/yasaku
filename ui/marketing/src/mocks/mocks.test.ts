import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vite-plus/test";
import { en } from "../content/en";
import { id } from "../content/id";
import ChatPreviewMock from "./ChatPreviewMock.vue";
import ConsoleShowcase from "./ConsoleShowcase.vue";
import FeatureVisual from "./FeatureVisual.vue";
import { formatIDR } from "./format";
import HomeMock from "./HomeMock.vue";
import PeriodCloseMock from "./PeriodCloseMock.vue";
import PeriodListMock from "./PeriodListMock.vue";

const variants = ["search", "wallets", "categories", "transfer", "adjust", "ledgers"] as const;

const all = (copy = en.mocks) => [
  mount(ConsoleShowcase, { props: { copy } }),
  mount(ConsoleShowcase, { props: { copy, compact: true } }),
  mount(PeriodCloseMock, { props: { copy } }),
  mount(ChatPreviewMock, { props: { copy } }),
  mount(HomeMock, { props: { copy } }),
  mount(PeriodListMock, { props: { copy } }),
  ...variants.map((variant) => mount(FeatureVisual, { props: { copy, variant } })),
];

describe("mocks", () => {
  it("hold no interactive element and no skeleton", () => {
    for (const wrapper of all()) {
      expect(wrapper.find("button, a, input, [tabindex]").exists()).toBe(false);
      expect(wrapper.html()).not.toMatch(/skeleton/);
    }
  });

  it("label the three pictures and hide their insides", () => {
    const cases = [
      [mount(ConsoleShowcase, { props: { copy: en.mocks } }), en.mocks.showcaseAlt],
      [mount(PeriodCloseMock, { props: { copy: en.mocks } }), en.mocks.periodAlt],
      [mount(ChatPreviewMock, { props: { copy: en.mocks } }), en.mocks.chatAlt],
      [mount(PeriodListMock, { props: { copy: en.mocks } }), en.mocks.periodsAlt],
    ] as const;
    for (const [wrapper, alt] of cases) {
      expect(wrapper.attributes("role")).toBe("img");
      expect(wrapper.attributes("aria-label")).toBe(alt);
      expect(wrapper.element.firstElementChild?.getAttribute("aria-hidden")).toBe("true");
    }
  });

  it("shows the Reports page with net, money in and out, and the Sankey", () => {
    const wrapper = mount(ConsoleShowcase, { props: { copy: en.mocks } });
    const text = wrapper.text();
    for (const label of [
      en.mocks.reports,
      en.mocks.netPeriod,
      en.mocks.moneyIn,
      en.mocks.moneyOut,
      en.mocks.whereMoneyWent,
    ]) {
      expect(text).toContain(label);
    }
    expect(text).toContain("yasaku.altalune.id/reports");
    expect(wrapper.find("[data-net]").text()).toBe(formatIDR(3_460_000));
    expect(wrapper.text()).toContain(formatIDR(9_700_000));
    expect(wrapper.text()).toContain(formatIDR(6_240_000));
  });

  it("draws the Sankey from income through wallets to every category, with no dashes", () => {
    const wrapper = mount(ConsoleShowcase, { props: { copy: en.mocks } });
    const svg = wrapper.find("svg.sankey");
    expect(svg.findAll("rect")).toHaveLength(2 + 1 + 2 + 6 + 1);
    expect(svg.findAll(".ribbons path")).toHaveLength(16);
    expect(svg.find("[stroke-dasharray]").exists()).toBe(false);
    for (const name of [
      en.mocks.categories.salary,
      en.mocks.wallets.bank,
      en.mocks.wallets.cash,
      en.mocks.saved,
      ...Object.values(en.mocks.categories).slice(0, 6),
    ]) {
      expect(svg.text()).toContain(name);
    }
    expect(svg.classes()).toEqual(expect.arrayContaining(["hidden", "lg:block"]));
    const mobile = wrapper.find(".lg\\:hidden");
    for (const name of Object.values(en.mocks.categories).slice(0, 6))
      expect(mobile.text()).toContain(name);
  });

  it("shows Home with quick add and entries by day", () => {
    const text = mount(HomeMock, { props: { copy: en.mocks } }).text();
    for (const label of [
      en.mocks.home,
      en.mocks.quickAdd,
      en.mocks.today,
      en.mocks.yesterday,
      en.mocks.tx.coffee,
    ]) {
      expect(text).toContain(label);
    }
  });

  it("renders the Indonesian labels", () => {
    const text = mount(ConsoleShowcase, { props: { copy: id.mocks } }).text();
    expect(text).toContain("Ke mana uangnya pergi");
    expect(text).toContain("Makan & Minum");
  });

  it("wraps long chat messages instead of overflowing the bubble", () => {
    for (const bubble of mount(ChatPreviewMock, { props: { copy: id.mocks } }).findAll(
      ".chat-bubble",
    )) {
      expect(bubble.classes()).toContain("break-words");
    }
  });

  it("marks the feature visuals decorative", () => {
    for (const variant of variants) {
      expect(
        mount(FeatureVisual, { props: { copy: en.mocks, variant } }).attributes("aria-hidden"),
      ).toBe("true");
    }
  });
});
