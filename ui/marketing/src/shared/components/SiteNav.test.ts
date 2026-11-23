import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { describe, expect, it } from "vite-plus/test";
import SiteNav from "./SiteNav.vue";

const props = {
  appName: "App",
  logoSrc: "/logo.svg",
  links: [
    { label: "Features", href: "#features" },
    { label: "FAQ", href: "#faq" },
  ],
  ctaLabel: "Get started",
  ctaHref: "https://app.example.com/login",
  locales: [
    { code: "EN", href: "/app/", hreflang: "en", name: "English", current: true },
    { code: "ID", href: "/app/id/", hreflang: "id", name: "Bahasa Indonesia", current: false },
  ],
  localeLabel: "Language",
  themeLabel: "Toggle theme",
  menuLabel: "Menu",
  github: { href: "https://github.com/example/app", label: "App on GitHub", stars: 120 },
};

describe("SiteNav", () => {
  it("renders real links for the CTA, sections, locales and GitHub", () => {
    const hrefs = mount(SiteNav, { props })
      .findAll("a")
      .map((a) => a.attributes("href"));
    for (const href of [
      props.ctaHref,
      "#features",
      "#faq",
      "/app/",
      "/app/id/",
      props.github.href,
    ]) {
      expect(hrefs).toContain(href);
    }
  });

  it("drops the motion toggle from the bar and the menu", () => {
    expect(mount(SiteNav, { props }).find('input[aria-label="Reduce motion"]').exists()).toBe(
      false,
    );
  });

  it("keeps GitHub, language and theme in the bar before the CTA", () => {
    const wrapper = mount(SiteNav, { props });
    const cluster = wrapper.find("[data-cluster]");
    expect(cluster.classes()).not.toContain("hidden");
    expect(cluster.find(`a[href="${props.github.href}"]`).exists()).toBe(true);
    expect(cluster.find('[aria-label="Language"]').exists()).toBe(true);
    expect(cluster.find('input[aria-label="Toggle theme"]').exists()).toBe(true);
    const end = wrapper.find(".navbar-end").element;
    expect(end.lastElementChild!.textContent).toContain(props.ctaLabel);
  });

  it("moves GitHub and the CTA into the mobile menu only where the bar hides them", () => {
    const wrapper = mount(SiteNav, { props });
    const menu = wrapper.find("details");
    expect(menu.classes()).toContain("lg:hidden");
    expect(menu.find(`a[href="${props.github.href}"]`).element.closest("li")!.className).toContain(
      "min-[420px]:hidden",
    );
    expect(menu.find(`a[href="${props.ctaHref}"]`).element.closest("li")!.className).toContain(
      "min-[560px]:hidden",
    );
    expect(wrapper.find(".navbar-end > a").classes()).toContain("max-[559px]:hidden");
  });

  it("links the brand to the top of the page", () => {
    expect(mount(SiteNav, { props }).find("[data-brand]").attributes("href")).toBe("#top");
  });

  it("switches to the desktop layout at lg, not md", () => {
    const wrapper = mount(SiteNav, { props });
    expect(wrapper.find(".navbar-center").classes()).toEqual(
      expect.arrayContaining(["hidden", "lg:flex"]),
    );
    expect(wrapper.html()).not.toMatch(/\bmd:(?:hidden|flex|inline-flex)\b/);
  });

  it("gives the desktop links a visible focus outline", () => {
    for (const a of mount(SiteNav, { props }).findAll(".navbar-center a")) {
      expect(a.classes()).toEqual(
        expect.arrayContaining(["focus-visible:outline-2", "focus-visible:outline-base-content"]),
      );
    }
  });

  it("lets a long wordmark truncate instead of pushing into the cluster", () => {
    const wrapper = mount(SiteNav, { props: { ...props, appName: "Longername Studio" } });
    const brand = wrapper.find("[data-brand]");
    expect(brand.classes()).toContain("min-w-0");
    expect(brand.find("img").classes()).toContain("shrink-0");
    expect(brand.find("[data-brand-name]").classes()).toEqual(
      expect.arrayContaining(["min-w-0", "truncate"]),
    );
    expect(wrapper.find(".navbar-start").classes()).toEqual(
      expect.arrayContaining(["min-w-0", "flex-1"]),
    );
  });

  it("never clips the mobile menu panel with an ancestor's overflow", () => {
    const wrapper = mount(SiteNav, { props });
    let el = wrapper.find("details .dropdown-content").element.parentElement;
    while (el && el !== wrapper.element.parentElement) {
      expect(
        [...el.classList].filter((c) => /(?:^|:)overflow-(?:[xy]-)?(?:hidden|clip)$/.test(c)),
        el.className,
      ).toEqual([]);
      el = el.parentElement;
    }
  });

  it("gives mobile menu links 44px touch targets", () => {
    for (const a of mount(SiteNav, { props }).findAll("details ul > li > a"))
      expect(a.classes().some((c) => c === "py-3" || c === "h-11")).toBe(true);
  });

  it("labels the mobile menu and closes it on a link click", async () => {
    const wrapper = mount(SiteNav, { props, attachTo: document.body });
    const details = wrapper.find("details");
    expect(details.find("summary").attributes("aria-label")).toBe("Menu");
    (details.element as HTMLDetailsElement).open = true;
    await details.find('a[href="#faq"]').trigger("click");
    expect((details.element as HTMLDetailsElement).open).toBe(false);
    wrapper.unmount();
  });

  it("closes the menu on Escape and returns focus to the summary", async () => {
    const wrapper = mount(SiteNav, { props, attachTo: document.body });
    const details = wrapper.find("details").element as HTMLDetailsElement;
    details.open = true;
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await nextTick();
    expect(details.open).toBe(false);
    expect(document.activeElement).toBe(details.querySelector("summary"));
    wrapper.unmount();
  });

  it("closes the menu on an outside click and returns focus to the summary", async () => {
    const wrapper = mount(SiteNav, { props, attachTo: document.body });
    const details = wrapper.find("details").element as HTMLDetailsElement;
    details.open = true;
    const outside = document.createElement("div");
    document.body.append(outside);
    outside.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true }));
    outside.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await nextTick();
    expect(details.open).toBe(false);
    expect(document.activeElement).toBe(details.querySelector("summary"));
    outside.remove();
    wrapper.unmount();
  });
});
