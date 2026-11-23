import { afterEach, describe, expect, it } from "vite-plus/test";
import { track } from "./track";

describe("track", () => {
  afterEach(() => {
    delete (window as { dataLayer?: unknown }).dataLayer;
  });

  it("pushes the event to dataLayer when a tag manager is present", () => {
    const layer: unknown[] = [];
    (window as { dataLayer?: unknown[] }).dataLayer = layer;
    track("copy_link_click");
    expect(layer).toEqual([{ event: "copy_link_click" }]);
  });

  it("does nothing without one", () => {
    expect(() => track("copy_link_click")).not.toThrow();
  });
});
