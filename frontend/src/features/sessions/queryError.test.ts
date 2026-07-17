import { describe, expect, it } from "vitest";

import { ErrorCode } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";

import { classifySessionsQueryError } from "./queryError";

function runtimeError(code: ErrorCode, field: string | null, retryable: boolean) {
  return {
    cause: JSON.stringify({
      error: { code, field, messageKey: "query.error.safe", retryable },
      version: "query-v1",
    }),
    message: "binding query failed",
  };
}

describe("Sessions safe query error classification", () => {
  it("maps only finite query-v1 evidence to recovery actions", () => {
    expect(classifySessionsQueryError(
      runtimeError(ErrorCode.ErrorValidation, "page.cursor", false),
    )).toEqual({ code: ErrorCode.ErrorValidation, recovery: "list_first_page", retryable: false });
    expect(classifySessionsQueryError(
      runtimeError(ErrorCode.ErrorValidation, "turnPage.cursor", false),
    )).toEqual({ code: ErrorCode.ErrorValidation, recovery: "turn_first_page", retryable: false });
    expect(classifySessionsQueryError(
      runtimeError(ErrorCode.ErrorNotFound, null, false),
    )).toEqual({ code: ErrorCode.ErrorNotFound, recovery: "close_detail", retryable: false });
    expect(classifySessionsQueryError(
      runtimeError(ErrorCode.ErrorUnavailable, null, true),
    )).toEqual({ code: ErrorCode.ErrorUnavailable, recovery: "retry", retryable: true });
  });

  it("fails unknown versions, malformed causes, and raw driver text closed to internal", () => {
    const secret = "file:///Users/example/private.db: driver exploded";
    for (const error of [
      new Error(secret),
      { cause: secret, message: secret },
      { cause: JSON.stringify({ version: "query-v2", error: { code: "not_found" } }) },
      { cause: { version: "query-v1", error: { code: "driver_failure", retryable: true } } },
    ]) {
      expect(classifySessionsQueryError(error)).toEqual({
        code: ErrorCode.ErrorInternal,
        recovery: "none",
        retryable: false,
      });
    }
  });
});
