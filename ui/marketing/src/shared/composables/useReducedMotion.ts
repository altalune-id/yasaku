import { usePreferredReducedMotion } from "@vueuse/core";
import { onMounted, type Ref, shallowRef, watch } from "vue";

const STORAGE_KEY = "altalune-motion";
const reduced = shallowRef(false);

function storedChoice(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

export function useReducedMotion(): Readonly<Ref<boolean>> {
  const preferred = usePreferredReducedMotion();

  onMounted(() => {
    reduced.value = document.documentElement.dataset.motion === "reduce";
  });

  watch(preferred, (value) => {
    if (value !== "reduce" && storedChoice()) return;
    const on = value === "reduce";
    document.documentElement.dataset.motion = on ? "reduce" : "full";
    reduced.value = on;
  });

  return reduced;
}

export function setReducedMotion(on: boolean): void {
  document.documentElement.dataset.motion = on ? "reduce" : "full";
  try {
    localStorage.setItem(STORAGE_KEY, on ? "reduce" : "full");
  } catch {}
  reduced.value = on;
}
