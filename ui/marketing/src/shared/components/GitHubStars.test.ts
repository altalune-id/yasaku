import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vite-plus/test";
import GitHubStars from "./GitHubStars.vue";

const base = { href: "https://github.com/example/app", label: "App on GitHub" };

describe("GitHubStars", () => {
  it("is an icon link with an accessible name", () => {
    const a = mount(GitHubStars, { props: { ...base, stars: null } }).find("a");
    expect(a.attributes("href")).toBe(base.href);
    expect(a.attributes("aria-label")).toBe(base.label);
    expect(a.find("svg").exists()).toBe(true);
  });

  it("shows the count only at or above the threshold", () => {
    expect(
      mount(GitHubStars, { props: { ...base, stars: null } })
        .find("[data-stars]")
        .exists(),
    ).toBe(false);
    expect(
      mount(GitHubStars, { props: { ...base, stars: 49 } })
        .find("[data-stars]")
        .exists(),
    ).toBe(false);
    expect(
      mount(GitHubStars, { props: { ...base, stars: 1234 } })
        .find("[data-stars]")
        .text(),
    ).toBe("1.2k");
  });
});
