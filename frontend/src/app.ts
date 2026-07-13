import { QueryClient, VueQueryPlugin } from "@tanstack/vue-query";
import { createApp, type App as VueApp } from "vue";
import type { Router, RouterHistory } from "vue-router";

import App from "./App.vue";
import { createAppI18n, type AppI18nOptions } from "./i18n";
import { createAppRouter } from "./router";

export interface AppDependencies {
  router: Router;
  i18n: ReturnType<typeof createAppI18n>;
  queryClient: QueryClient;
}

export interface CreateAppDependenciesOptions extends AppI18nOptions {
  history?: RouterHistory;
  queryClient?: QueryClient;
}

export function createAppQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        refetchOnWindowFocus: false,
      },
    },
  });
}

export function createAppDependencies(options: CreateAppDependenciesOptions = {}): AppDependencies {
  return {
    router: createAppRouter(options.history),
    i18n: createAppI18n({ onMissing: options.onMissing }),
    queryClient: options.queryClient ?? createAppQueryClient(),
  };
}

export function createCodexPulseApp(
  dependencies: AppDependencies = createAppDependencies(),
): VueApp {
  const app = createApp(App);

  app.use(dependencies.router);
  app.use(dependencies.i18n);
  app.use(VueQueryPlugin, { queryClient: dependencies.queryClient });

  return app;
}
