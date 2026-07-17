import { describe, expect, it } from "vitest";

import { ErrorCode } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";

import { classifyProjectsQueryError } from "./queryError";

function runtimeError(code: ErrorCode, field: string | null, retryable: boolean) {
  return {
    cause: JSON.stringify({
      error: { code, field, messageKey: "query.error.safe", retryable },
      version: "query-v1",
    }),
    message: "binding query failed",
  };
}

describe("Projects safe query error classification", () => {
  it("maps each cursor scope, not-found, and unavailable to a finite recovery", () => {
    expect(classifyProjectsQueryError(
      runtimeError(ErrorCode.ErrorValidation, "page.cursor", false),
    )).toEqual({ code: ErrorCode.ErrorValidation, recovery: "list_first_page", retryable: false });
    expect(classifyProjectsQueryError(
      runtimeError(ErrorCode.ErrorValidation, "sessionPage.cursor", false),
    )).toEqual({ code: ErrorCode.ErrorValidation, recovery: "session_first_page", retryable: false });
    expect(classifyProjectsQueryError(
      runtimeError(ErrorCode.ErrorValidation, "modelPage.cursor", false),
    )).toEqual({ code: ErrorCode.ErrorValidation, recovery: "model_first_page", retryable: false });
    expect(classifyProjectsQueryError(
      runtimeError(ErrorCode.ErrorNotFound, null, false),
    )).toEqual({ code: ErrorCode.ErrorNotFound, recovery: "close_detail", retryable: false });
    expect(classifyProjectsQueryError(
      runtimeError(ErrorCode.ErrorUnavailable, null, true),
    )).toEqual({ code: ErrorCode.ErrorUnavailable, recovery: "retry", retryable: true });
  });

  it("fails malformed versions and raw driver text closed without exposing the cause", () => {
    const privateCause = "file:///Users/example/private.db: driver exploded";
    for (const error of [
      new Error(privateCause),
      { cause: privateCause, message: privateCause },
      { cause: JSON.stringify({ version: "query-v2", error: { code: "not_found" } }) },
      { cause: { version: "query-v1", error: { code: "driver_failure", retryable: true } } },
    ]) {
      expect(classifyProjectsQueryError(error)).toEqual({
        code: ErrorCode.ErrorInternal,
        recovery: "none",
        retryable: false,
      });
    }
  });
});
