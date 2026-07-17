import { onScopeDispose, readonly, ref } from "vue";

import { createOverviewPresetRange } from "@/features/overview/range";

const maximumLocalDayMilliseconds = 36 * 60 * 60 * 1_000;

function localDate(nowMS: number, timeZone: string) {
  return createOverviewPresetRange("today", nowMS, timeZone).startDate;
}

function millisecondsUntilNextLocalDate(nowMS: number, timeZone: string) {
  const currentDate = localDate(nowMS, timeZone);
  let lower = nowMS;
  let upper = nowMS + maximumLocalDayMilliseconds;
  if (localDate(upper, timeZone) === currentDate) {
    throw new Error("invalid local date clock");
  }

  while (upper - lower > 1) {
    const middle = lower + Math.floor((upper - lower) / 2);
    if (localDate(middle, timeZone) === currentDate) {
      lower = middle;
    } else {
      upper = middle;
    }
  }
  return Math.max(1, upper - nowMS);
}

export function useLocalDateClock(
  timeZone: string,
  now: () => number = Date.now,
) {
  const nowMS = ref(now());
  let timer: ReturnType<typeof setTimeout> | undefined;

  function scheduleNextBoundary() {
    const current = now();
    nowMS.value = current;
    timer = setTimeout(
      scheduleNextBoundary,
      millisecondsUntilNextLocalDate(current, timeZone),
    );
  }

  scheduleNextBoundary();
  onScopeDispose(() => {
    if (timer !== undefined) clearTimeout(timer);
  });
  return readonly(nowMS);
}
