import { type MaybeElementRef, unrefElement, useIntersectionObserver } from "@vueuse/core";
import { onMounted, type Ref, shallowRef, watch } from "vue";

export function useInView(
  target: MaybeElementRef,
  options: { once?: boolean } = {},
): Readonly<Ref<boolean>> {
  const inView = shallowRef(false);
  const mounted = shallowRef(false);

  const { isSupported, stop } = useIntersectionObserver(
    target,
    ([entry]) => {
      if (options.once && inView.value) return;
      inView.value = entry?.isIntersecting ?? false;
      if (options.once && inView.value) stop();
    },
    { threshold: 0.1 },
  );

  onMounted(() => {
    mounted.value = true;
    if (!isSupported.value) inView.value = true;
  });

  watch(
    [mounted, inView, () => unrefElement(target)],
    ([isMounted, visible, el]) => {
      if (!isMounted || !isSupported.value || !el) return;
      el.setAttribute("data-inview", String(visible));
    },
    { flush: "post" },
  );

  return inView;
}
