import { onScopeDispose, readonly, ref } from "vue";

export function useQuotaDisplayClock(now: () => number = Date.now) {
  const nowMS = ref(now());
  const timer = setInterval(() => {
    nowMS.value = now();
  }, 1_000);
  onScopeDispose(() => clearInterval(timer));
  return readonly(nowMS);
}
