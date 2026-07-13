<script setup lang="ts">
import { useQuery } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";

import { bootstrapQueryOptions } from "@/queries/bootstrap";

const { t } = useI18n();
const { data, isError, isPending, refetch } = useQuery(bootstrapQueryOptions());

function retryBinding() {
  void refetch();
}
</script>

<template>
  <main class="min-h-screen bg-slate-950/85 px-8 py-7 text-slate-100">
    <div class="mx-auto flex min-h-[calc(100vh-3.5rem)] max-w-5xl flex-col">
      <header class="flex items-center justify-between border-b border-white/10 pb-5">
        <div>
          <p class="text-xs font-semibold tracking-[0.18em] text-sky-300 uppercase">
            {{ t("app.shellReady") }}
          </p>
          <h1 class="mt-2 text-2xl font-semibold tracking-tight">
            {{ data?.name ?? t("app.name") }}
          </h1>
        </div>
        <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs text-slate-300">
          {{ t("app.description") }}
        </span>
      </header>

      <section class="flex flex-1 items-center justify-center" aria-live="polite">
        <div class="w-full max-w-xl rounded-3xl border border-white/10 bg-white/5 p-8 shadow-2xl shadow-black/20">
          <p class="text-sm leading-6 text-slate-300">
            {{ t("app.shellDescription") }}
          </p>

          <div
            v-if="isPending"
            data-testid="service-loading"
            class="mt-6 rounded-2xl border border-sky-400/20 bg-sky-400/10 px-4 py-3 text-sm text-sky-100"
          >
            {{ t("app.service.loading") }}
          </div>

          <div
            v-else-if="isError"
            data-testid="service-error"
            class="mt-6 rounded-2xl border border-rose-400/20 bg-rose-400/10 px-4 py-4 text-sm text-rose-100"
          >
            <p>{{ t("app.service.error") }}</p>
            <button
              data-testid="retry-binding"
              type="button"
              class="mt-3 rounded-full border border-rose-200/30 px-4 py-1.5 font-medium hover:bg-rose-100/10 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-rose-200"
              @click="retryBinding"
            >
              {{ t("app.service.retry") }}
            </button>
          </div>

          <div
            v-else
            data-testid="service-ready"
            class="mt-6 rounded-2xl border border-emerald-400/20 bg-emerald-400/10 px-4 py-4 text-sm text-emerald-50"
          >
            <p class="font-medium">{{ t("app.service.ready") }}</p>
            <dl class="mt-4 grid grid-cols-2 gap-3 text-xs text-emerald-100/80">
              <div>
                <dt>{{ t("app.metadata.platform") }}</dt>
                <dd class="mt-1 font-mono text-emerald-50">{{ data?.platform }}</dd>
              </div>
              <div>
                <dt>{{ t("app.metadata.locale") }}</dt>
                <dd class="mt-1 font-mono text-emerald-50">{{ data?.locale }}</dd>
              </div>
            </dl>
          </div>
        </div>
      </section>
    </div>
  </main>
</template>
