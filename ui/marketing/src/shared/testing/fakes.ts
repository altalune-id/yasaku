export class FakeIntersectionObserver {
  static instances: FakeIntersectionObserver[] = [];
  readonly targets: Element[] = [];

  constructor(private readonly callback: IntersectionObserverCallback) {
    FakeIntersectionObserver.instances.push(this);
  }

  observe(target: Element) {
    this.targets.push(target);
  }

  unobserve() {}

  disconnect() {
    this.targets.length = 0;
  }

  takeRecords() {
    return [];
  }

  static trigger(isIntersecting: boolean) {
    for (const io of FakeIntersectionObserver.instances) {
      const entries = io.targets.map(
        (target) => ({ target, isIntersecting }) as IntersectionObserverEntry,
      );
      if (entries.length) io.callback(entries, io as unknown as IntersectionObserver);
    }
  }
}

export function installIntersectionObserver() {
  FakeIntersectionObserver.instances = [];
  const original = globalThis.IntersectionObserver;
  globalThis.IntersectionObserver =
    FakeIntersectionObserver as unknown as typeof IntersectionObserver;
  return () => {
    globalThis.IntersectionObserver = original;
  };
}

export function removeIntersectionObserver() {
  const original = globalThis.IntersectionObserver;
  delete (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver;
  return () => {
    globalThis.IntersectionObserver = original;
  };
}
