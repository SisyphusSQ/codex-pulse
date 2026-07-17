import { ErrorCode } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";

export type SessionsErrorRecovery =
  | "close_detail"
  | "list_first_page"
  | "none"
  | "retry"
  | "turn_first_page";

export interface SessionsQueryError {
  code: ErrorCode;
  recovery: SessionsErrorRecovery;
  retryable: boolean;
}

interface UnknownRecord {
  [key: string]: unknown;
}

const finiteCodes = new Set<ErrorCode>([
  ErrorCode.ErrorValidation,
  ErrorCode.ErrorNotFound,
  ErrorCode.ErrorPartial,
  ErrorCode.ErrorUnavailable,
  ErrorCode.ErrorCancelled,
  ErrorCode.ErrorDeadlineExceeded,
  ErrorCode.ErrorInternal,
]);

const internalError: SessionsQueryError = Object.freeze({
  code: ErrorCode.ErrorInternal,
  recovery: "none",
  retryable: false,
});

function record(value: unknown): UnknownRecord | null {
  return typeof value === "object" && value !== null ? value as UnknownRecord : null;
}

function parseCause(value: unknown): UnknownRecord | null {
  if (typeof value === "string") {
    try {
      return record(JSON.parse(value));
    } catch {
      return null;
    }
  }
  return record(value);
}

export function classifySessionsQueryError(value: unknown): SessionsQueryError {
  const outer = record(value);
  const envelope = parseCause(outer?.cause ?? value);
  const detail = record(envelope?.error);
  if (
    envelope?.version !== "query-v1"
    || typeof detail?.code !== "string"
    || !finiteCodes.has(detail.code as ErrorCode)
    || typeof detail.retryable !== "boolean"
    || !(detail.field === null || typeof detail.field === "string")
  ) {
    return internalError;
  }

  const code = detail.code as ErrorCode;
  let recovery: SessionsErrorRecovery = "none";
  if (code === ErrorCode.ErrorValidation && detail.field === "page.cursor") {
    recovery = "list_first_page";
  } else if (code === ErrorCode.ErrorValidation && detail.field === "turnPage.cursor") {
    recovery = "turn_first_page";
  } else if (code === ErrorCode.ErrorNotFound) {
    recovery = "close_detail";
  } else if (
    code === ErrorCode.ErrorUnavailable
    || code === ErrorCode.ErrorDeadlineExceeded
    || code === ErrorCode.ErrorPartial
  ) {
    recovery = "retry";
  }

  return { code, recovery, retryable: detail.retryable };
}
