// A busy service delays a pending edit instead of dropping it. resume() is
// called after requests/polls; it never bypasses the original debounce timer.
export function createAutoBuildScheduler(ready: () => boolean, build: () => void, delay = 600) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let pending = false;
  const cancel = () => {
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
    pending = false;
  };
  const resume = () => {
    if (pending && timer === undefined && ready()) {
      pending = false;
      build();
    }
  };
  return {
    cancel,
    resume,
    schedule() {
      cancel();
      pending = true;
      timer = setTimeout(() => {
        timer = undefined;
        resume();
      }, delay);
    },
  };
}
