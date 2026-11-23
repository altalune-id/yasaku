import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vite-plus/test";
import FaqList from "./FaqList.vue";

const items = [
  { q: "Q1", a: "A1" },
  { q: "Q2", a: "A2" },
  { q: "Q3", a: "A3" },
];

describe("FaqList", () => {
  it("renders one details per item", () => {
    const wrapper = mount(FaqList, { props: { title: "FAQ", items } });
    const details = wrapper.findAll("details");
    expect(details).toHaveLength(3);
    expect(details.map((d) => d.find("summary").text())).toEqual(["Q1", "Q2", "Q3"]);
  });

  it("replaces only answer 2 with slot item-2", () => {
    const wrapper = mount(FaqList, {
      props: { title: "FAQ", items },
      slots: { "item-2": '<a href="https://x.test">rich</a>' },
    });
    const details = wrapper.findAll("details");
    expect(details[0]!.text()).toContain("A1");
    expect(details[1]!.text()).not.toContain("A2");
    expect(details[1]!.find("a").attributes("href")).toBe("https://x.test");
    expect(details[2]!.text()).toContain("A3");
  });
});
