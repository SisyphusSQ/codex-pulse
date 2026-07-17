import { flushPromises, mount } from "@vue/test-utils";
import { ref } from "vue";
import { createMemoryHistory } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  ErrorCode,
  NumericUnit,
  ResponseStatus,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import { createAppI18n } from "@/i18n";
import { createAppRouter } from "@/router";

import ProjectsView from "./ProjectsView.vue";

const projectsHarness = vi.hoisted(() => ({
  capturedModelCursor: null as unknown,
  capturedSessionCursor: null as unknown,
  capturedState: null as unknown,
  queries: {} as Record<string, unknown>,
}));

vi.mock("@/features/projects/useProjectQueries", () => ({
  useProjectQueries: (state: unknown, sessionCursor: unknown, modelCursor: unknown) => {
    projectsHarness.capturedState = state;
    projectsHarness.capturedSessionCursor = sessionCursor;
    projectsHarness.capturedModelCursor = modelCursor;
    return projectsHarness.queries;
  },
}));

const known = (value: number, unit = NumericUnit.NumericTokens) => ({ value, unit, unknownReason: null });
const attribution = (id: string | null, displayName: string | null, confidence = "high") => ({
  confidence,
  displayName,
  id,
  reason: confidence === "unknown" ? "missing" : "root_matched",
  source: confidence === "unknown" ? "missing" : "registered_root",
});
const totals = (tokens = 24_000, cost = 1_000_000) => ({
  cachedInputTokens: known(8_000),
  estimatedUsdMicros: known(cost, NumericUnit.NumericMicroUSD),
  firstActivityAtMs: known(Date.parse("2026-07-15T06:00:00Z"), NumericUnit.NumericMilliseconds),
  inputTokens: known(10_000),
  lastActivityAtMs: known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
  outputTokens: known(4_000),
  pricedTurnCount: known(1, NumericUnit.NumericCount),
  reasoningTokens: known(2_000),
  totalTokens: known(tokens),
  turnCount: known(2, NumericUnit.NumericCount),
  unpricedTurnCount: known(1, NumericUnit.NumericCount),
});
const daily = () => ({
  bucketStartAtMs: known(Date.parse("2026-07-15T16:00:00Z"), NumericUnit.NumericMilliseconds),
  confidence: "high",
  reason: "root_matched",
  source: "registered_root",
  totals: totals(),
});
const item = {
  dimensionKey: "opaque-project-a",
  project: attribution("opaque-project-a", "Codex Pulse"),
  sessionCount: known(2, NumericUnit.NumericCount),
  totals: totals(),
  trend: [daily()],
};
const meta = (status = ResponseStatus.ResponseComplete, page = { hasMore: true, limit: 50, nextCursor: "opaque-list-next" }) => ({
  issues: status === ResponseStatus.ResponseComplete ? null : [{
    code: ErrorCode.ErrorPartial,
    messageKey: "query.error.partial",
    retryable: true,
  }],
  page,
  status,
  version: "query-v1",
});
const listResponse = (status = ResponseStatus.ResponseComplete, items = [item]) => ({
  currency: "USD",
  globalTotals: totals(100_000, 4_000_000),
  items,
  matchedCount: known(items.length, NumericUnit.NumericCount),
  matchedTotals: totals(24_000, 1_000_000),
  meta: meta(status),
  pageTotals: totals(24_000, 1_000_000),
  pricingSource: "OpenAI public pricing",
  pricingVersions: ["2026.06"],
  range: {
    endAtMs: Date.parse("2026-07-17T16:00:00Z"),
    startAtMs: Date.parse("2026-07-10T16:00:00Z"),
    timeZone: "Asia/Shanghai",
  },
  reportingTimeZone: "Asia/Shanghai",
});
const detailResponse = () => ({
  currency: "USD",
  daily: [daily()],
  globalTotals: totals(100_000, 4_000_000),
  item,
  meta: { ...meta(ResponseStatus.ResponsePartial), page: null },
  modelPage: { hasMore: true, limit: 20, nextCursor: "opaque-model-next" },
  models: [{ dimensionKey: "opaque-model", model: attribution("opaque-model", "GPT-5"), totals: totals(16_000, 700_000) }],
  pricingSource: "OpenAI public pricing",
  pricingVersions: ["2026.06"],
  range: listResponse().range,
  reportingTimeZone: "Asia/Shanghai",
  sessionPage: { hasMore: true, limit: 20, nextCursor: "opaque-session-next" },
  sessions: [{
    activity: "active",
    displayTitle: "实现 Projects 页面",
    lastActivityAtMs: known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
    model: attribution("opaque-model", "GPT-5"),
    sessionId: "opaque-session",
    titleConfidence: "high",
    titleReason: "stable_identity",
    titleSource: "session_id_fallback",
    totals: totals(12_000, 500_000),
  }],
});

function requestState(endDateExclusive = "2026-07-17") {
  return ref({
    detail: null,
    list: {
      timeRange: {
        endDateExclusive,
        startDate: endDateExclusive === "2026-07-17" ? "2026-07-10" : "2026-07-11",
        timeZone: "Asia/Shanghai",
      },
    },
  });
}

function query(data: unknown, error: unknown = null, overrides: Record<string, boolean> = {}) {
  return {
    data: ref(data),
    error: ref(error),
    isError: ref(error !== null),
    isFetching: ref(false),
    isPending: ref(data === undefined && error === null),
    isPlaceholderData: ref(false),
    refetch: vi.fn(),
    ...Object.fromEntries(Object.entries(overrides).map(([key, value]) => [key, ref(value)])),
  };
}

function runtimeError(code: ErrorCode, field: string | null) {
  return {
    cause: JSON.stringify({
      error: { code, field, messageKey: "query.error.safe", retryable: code === ErrorCode.ErrorUnavailable },
      version: "query-v1",
    }),
    message: "binding query failed",
  };
}

async function renderProjects(path = "/projects?project=opaque-project-a") {
  const router = createAppRouter(createMemoryHistory());
  await router.push(path);
  await router.isReady();
  const wrapper = mount(ProjectsView, {
    global: {
      plugins: [createAppI18n(), router],
      stubs: { ProjectTrendChart: { template: "<div data-testid='project-trend' />" } },
    },
  });
  await flushPromises();
  return { router, wrapper };
}

describe("ProjectsView", () => {
  beforeEach(() => {
    projectsHarness.queries = {
      detail: query(detailResponse()),
      list: query(listResponse()),
      requests: requestState(),
    };
  });

  it("keeps list scope in URL while both detail cursors remain ephemeral and independent", async () => {
    const { router, wrapper } = await renderProjects();

    expect(wrapper.findAll("[data-testid='project-row']")).toHaveLength(1);
    expect(wrapper.text()).toContain("匹配 1 个项目");
    expect(wrapper.text()).toContain("$4.00");
    expect(wrapper.text()).toContain("模型贡献");

    await wrapper.get("[data-testid='projects-confidence']").setValue("unknown");
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({ confidence: "unknown" });

    await wrapper.get("[data-testid='project-row']").trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({ confidence: "unknown", project: "opaque-project-a" });

    const sessionNext = wrapper.get("[data-testid='project-session-next']");
    await Promise.all([sessionNext.trigger("click"), sessionNext.trigger("click")]);
    expect((projectsHarness.capturedSessionCursor as { value: string | null }).value).toBe("opaque-session-next");
    expect((projectsHarness.capturedModelCursor as { value: string | null }).value).toBeNull();

    const detailQuery = projectsHarness.queries.detail as ReturnType<typeof query>;
    detailQuery.isFetching.value = true;
    await flushPromises();
    detailQuery.isFetching.value = false;
    await flushPromises();
    await wrapper.get("[data-testid='project-model-next']").trigger("click");
    expect((projectsHarness.capturedModelCursor as { value: string | null }).value).toBe("opaque-model-next");
    expect(router.currentRoute.value.query).not.toHaveProperty("modelCursor");
    expect(router.currentRoute.value.query).not.toHaveProperty("sessionCursor");

    detailQuery.isFetching.value = true;
    await flushPromises();
    detailQuery.isFetching.value = false;
    await flushPromises();
    await wrapper.get("[data-testid='project-session-previous']").trigger("click");
    expect((projectsHarness.capturedSessionCursor as { value: string | null }).value).toBeNull();
    expect((projectsHarness.capturedModelCursor as { value: string | null }).value).toBe("opaque-model-next");

    const listNext = wrapper.get("[data-testid='projects-list-next']");
    await Promise.all([listNext.trigger("click"), listNext.trigger("click")]);
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({ confidence: "unknown", cursor: "opaque-list-next" });
    await wrapper.get("[data-testid='projects-list-previous']").trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({ confidence: "unknown" });

    await wrapper.get("[data-testid='projects-list-next']").trigger("click");
    await flushPromises();
    router.back();
    await flushPromises();
    expect(wrapper.get("[data-testid='projects-list-previous']").attributes()).toHaveProperty("disabled");
  });

  it("normalizes invalid URL state and keeps partial known-empty explicit", async () => {
    projectsHarness.queries = {
      detail: query(undefined),
      list: query(listResponse(ResponseStatus.ResponsePartial, [])),
      requests: requestState(),
    };
    const { router, wrapper } = await renderProjects("/projects?range=forever&ignored=secret");

    expect(router.currentRoute.value.query).toEqual({});
    expect(wrapper.get("[data-testid='projects-normalized']").text()).toContain("恢复为默认值");
    expect(wrapper.get("[data-testid='projects-list-empty']").text()).toContain("暂无项目");
    expect(wrapper.text()).toContain("部分数据");
    expect(wrapper.text()).not.toContain("secret");
  });

  it("does not steal focus on URL restore and focuses detail after a user selection", async () => {
    const focus = vi.spyOn(HTMLElement.prototype, "focus");
    const { wrapper } = await renderProjects();

    expect(focus).not.toHaveBeenCalled();
    const row = wrapper.get("[data-testid='project-row']").element;
    await wrapper.get("[data-testid='project-detail-close']").trigger("click");
    await flushPromises();
    expect(focus).toHaveBeenCalledOnce();
    expect(focus.mock.instances[0]).toBe(row);
    await wrapper.get("[data-testid='project-row']").trigger("click");
    await flushPromises();
    expect(focus).toHaveBeenCalledTimes(2);
    expect(focus.mock.instances[1]).toBe(wrapper.get("#project-detail-title").element);
    focus.mockRestore();
  });

  it("focuses detail only after an asynchronous user selection response arrives", async () => {
    const detail = query(undefined);
    projectsHarness.queries = { detail, list: query(listResponse()), requests: requestState() };
    const focus = vi.spyOn(HTMLElement.prototype, "focus");
    const { wrapper } = await renderProjects("/projects");

    await wrapper.get("[data-testid='project-row']").trigger("click");
    await flushPromises();
    expect(focus).not.toHaveBeenCalled();
    detail.data.value = detailResponse();
    detail.isPending.value = false;
    await flushPromises();
    expect(focus).toHaveBeenCalledOnce();
    expect(focus.mock.instances[0]).toBe(wrapper.get("#project-detail-title").element);
    focus.mockRestore();
  });

  it("keeps trusted detail visible on background failure and recovers finite cursor errors", async () => {
    const detail = query(detailResponse(), runtimeError(ErrorCode.ErrorUnavailable, null));
    const list = query(listResponse());
    projectsHarness.queries = { detail, list, requests: requestState() };
    const { router, wrapper } = await renderProjects();

    expect(wrapper.get("[data-testid='projects-detail-stale']").text()).toContain("上次可信数据");
    expect(wrapper.text()).toContain("模型贡献");
    expect(wrapper.text()).not.toContain("binding query failed");
    await wrapper.get("[data-testid='projects-detail-stale-retry']").trigger("click");
    expect(detail.refetch).toHaveBeenCalledOnce();

    list.data.value = undefined;
    list.error.value = runtimeError(ErrorCode.ErrorValidation, "page.cursor");
    list.isError.value = true;
    await router.push("/projects?cursor=tampered&project=opaque-project-a");
    await flushPromises();
    await wrapper.get("[data-testid='projects-list-recover']").trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({});

    list.data.value = listResponse();
    list.error.value = null;
    list.isError.value = false;
    detail.data.value = undefined;
    detail.error.value = runtimeError(ErrorCode.ErrorNotFound, null);
    detail.isError.value = true;
    await router.push("/projects?project=opaque-project-a");
    await flushPromises();
    await wrapper.get("[data-testid='projects-detail-recover']").trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({});
  });

  it("recovers Session and Model contribution cursors independently", async () => {
    const detail = query(detailResponse());
    projectsHarness.queries = { detail, list: query(listResponse()), requests: requestState() };
    const { wrapper } = await renderProjects();

    await wrapper.get("[data-testid='project-session-next']").trigger("click");
    detail.isFetching.value = true;
    await flushPromises();
    detail.isFetching.value = false;
    await flushPromises();
    await wrapper.get("[data-testid='project-model-next']").trigger("click");
    expect((projectsHarness.capturedSessionCursor as { value: string | null }).value).toBe("opaque-session-next");
    expect((projectsHarness.capturedModelCursor as { value: string | null }).value).toBe("opaque-model-next");

    detail.data.value = undefined;
    detail.error.value = runtimeError(ErrorCode.ErrorValidation, "sessionPage.cursor");
    detail.isError.value = true;
    await flushPromises();
    await wrapper.get("[data-testid='projects-detail-recover']").trigger("click");
    expect((projectsHarness.capturedSessionCursor as { value: string | null }).value).toBeNull();
    expect((projectsHarness.capturedModelCursor as { value: string | null }).value).toBe("opaque-model-next");

    detail.error.value = runtimeError(ErrorCode.ErrorValidation, "modelPage.cursor");
    await flushPromises();
    await wrapper.get("[data-testid='projects-detail-recover']").trigger("click");
    expect((projectsHarness.capturedModelCursor as { value: string | null }).value).toBeNull();
  });

  it("clears list, Session, and Model cursor scopes when the resolved local date range rolls over", async () => {
    const { router, wrapper } = await renderProjects("/projects");

    await wrapper.get("[data-testid='projects-list-next']").trigger("click");
    await flushPromises();
    await wrapper.get("[data-testid='project-row']").trigger("click");
    await flushPromises();
    await wrapper.get("[data-testid='project-session-next']").trigger("click");
    const detailQuery = projectsHarness.queries.detail as ReturnType<typeof query>;
    detailQuery.isFetching.value = true;
    await flushPromises();
    detailQuery.isFetching.value = false;
    await flushPromises();
    await wrapper.get("[data-testid='project-model-next']").trigger("click");

    expect(router.currentRoute.value.query).toEqual({
      cursor: "opaque-list-next",
      project: "opaque-project-a",
    });
    expect((projectsHarness.capturedSessionCursor as { value: string | null }).value)
      .toBe("opaque-session-next");
    expect((projectsHarness.capturedModelCursor as { value: string | null }).value)
      .toBe("opaque-model-next");

    (projectsHarness.queries.requests as ReturnType<typeof requestState>).value = requestState("2026-07-18").value;
    await flushPromises();

    expect(router.currentRoute.value.query).toEqual({});
    expect(wrapper.get("[data-testid='projects-list-previous']").attributes()).toHaveProperty("disabled");
    expect((projectsHarness.capturedSessionCursor as { value: string | null }).value).toBeNull();
    expect((projectsHarness.capturedModelCursor as { value: string | null }).value).toBeNull();
  });
});
