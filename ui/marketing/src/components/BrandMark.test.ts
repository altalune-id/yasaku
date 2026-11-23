import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vite-plus/test";
import BrandMark from "./BrandMark.vue";

describe("BrandMark", () => {
  it("draws the console mark in currentColor with the brand token", () => {
    const svg = mount(BrandMark).find("svg");
    expect(svg.attributes("stroke")).toBe("currentColor");
    expect(svg.attributes("aria-hidden")).toBe("true");
    expect(svg.classes()).toContain("text-brand");
    expect(svg.findAll("path")).toHaveLength(4);
  });
});
