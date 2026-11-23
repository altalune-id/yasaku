import { onMounted, onUnmounted, readonly, type Ref, shallowRef, watch } from "vue";

export interface Cue {
  at: number;
  run: () => void;
}

export function useTimeline(opts: {
  intro?: Cue[];
  introMs?: number;
  loop: Cue[];
  loopMs: number;
  inView: Readonly<Ref<boolean>>;
  reduced: Readonly<Ref<boolean>>;
  onReset: () => void;
}): { running: Readonly<Ref<boolean>> } {
  const running = shallowRef(false);
  let introDone = !opts.intro?.length;
  let timer: ReturnType<typeof setTimeout> | undefined;

  function play(cues: Cue[], length: number, then: () => void, defer: boolean) {
    const sorted = [...cues].sort((a, b) => a.at - b.at);
    const step = (i: number, now: number) => {
      const cue = sorted[i];
      if (!cue) {
        timer = setTimeout(then, Math.max(0, length - now));
        return;
      }
      const fire = () => {
        cue.run();
        step(i + 1, cue.at);
      };
      if (cue.at === now && !(defer && i === 0)) return fire();
      timer = setTimeout(fire, cue.at - now);
    };
    step(0, 0);
  }

  function loop(defer = false) {
    opts.onReset();
    play(opts.loop, opts.loopMs, () => loop(), defer);
  }

  function start() {
    running.value = true;
    if (introDone) return loop(true);
    play(
      opts.intro!,
      opts.introMs ?? 0,
      () => {
        introDone = true;
        loop();
      },
      true,
    );
  }

  function stop() {
    clearTimeout(timer);
    timer = undefined;
    running.value = false;
    opts.onReset();
  }

  onMounted(() =>
    watch(
      [opts.inView, opts.reduced],
      ([inView, reduced]) => {
        const want = inView && !reduced;
        if (want && !running.value) start();
        if (!want && running.value) stop();
      },
      { immediate: true },
    ),
  );
  onUnmounted(() => clearTimeout(timer));

  return { running: readonly(running) };
}
