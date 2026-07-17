import { ErrorCode } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";

export type ProjectsErrorRecovery =
  | "close_detail"
  | "list_first_page"
  | "model_first_page"
  | "none"
  | "retry"
  | "session_first_page";

export interface ProjectsQueryError {
  code: ErrorCode;
  recovery: ProjectsErrorRecovery;
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

const internalError: ProjectsQueryError = Object.freeze({
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

export function classifyProjectsQueryError(value: unknown): ProjectsQueryError {
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
  let recovery: ProjectsErrorRecovery = "none";
  if (code === ErrorCode.ErrorValidation && detail.field === "page.cursor") {
    recovery = "list_first_page";
  } else if (code === ErrorCode.ErrorValidation && detail.field === "sessionPage.cursor") {
    recovery = "session_first_page";
  } else if (code === ErrorCode.ErrorValidation && detail.field === "modelPage.cursor") {
    recovery = "model_first_page";
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
