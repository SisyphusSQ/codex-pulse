#!/usr/bin/env bash

set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

document=docs/test/m11-e1.md
if [[ -n "${M11_MATRIX_DOC+x}" ]]; then
  if [[ "${M11_MATRIX_TEST_MODE:-}" != "1" ]]; then
    echo "M11-001: canonical acceptance gate cannot be redirected" >&2
    exit 1
  fi
  document=$M11_MATRIX_DOC
fi

if [[ ! -f "$document" ]]; then
  echo "M11-002: acceptance matrix document is missing: $document" >&2
  exit 1
fi

required_tokens=(
  '## 当前验证结果'
  '## 执行副作用'
  '## 统一场景矩阵'
  '## 阻塞、失败回写与重测规则'
  '## 清理与提交版证据边界'
  '自动化'
  '人工 macOS'
  '真实数据'
  '发布 E2E'
  'actions_disabled_by_user'
  '正式发布仍需用户另行明确授权'
)

failed=0
for token in "${required_tokens[@]}"; do
  if ! grep -Fq -- "$token" "$document"; then
    echo "M11-003: acceptance contract is missing required token: $token" >&2
    failed=1
  fi
done

# spec format: scenario ID|required responsibility|exact execution type|required result class|required evidence token
matrix_specs=(
  'ONB-01|TOO-298|真实数据 + 自动化|PASS|docs/test/m11-e2.md'
  'IDX-01|TOO-298|真实数据 + 自动化|PASS|docs/test/m11-e2.md'
  'IDX-02|TOO-298|真实数据 + 自动化|PASS|docs/test/m11-e2.md'
  'IDX-03|TOO-298|自动化 + 真实数据|PASS|docs/test/m11-e2.md'
  'LED-01|TOO-298|真实数据 + 自动化|PASS|docs/test/m11-e2.md'
  'QUO-01|TOO-298|真实数据 + 自动化|PASS|docs/test/m11-e2.md'
  'QUO-02|TOO-298|自动化 + 真实数据|PASS|docs/test/m11-e2.md'
  'QUO-03|TOO-298 + TOO-301|自动化 + 人工 macOS|PARTIAL PASS|docs/test/m11-e2.md'
  'UI-01|TOO-298 + TOO-301|自动化 + 人工 macOS|PARTIAL PASS|docs/test/m11-e2.md'
  'UI-02|TOO-298 + TOO-301|真实数据 + 自动化 + 人工 macOS|PARTIAL PASS|docs/test/m11-e2.md'
  'TRY-01|TOO-301|人工 macOS + 真实数据|EXCLUDED|TOO-301 用户取消'
  'TRY-02|TOO-301|人工 macOS|EXCLUDED|TOO-301 用户取消'
  'HLT-01|TOO-298 + TOO-301|自动化 + 人工 macOS|PARTIAL PASS|docs/test/m11-e2.md'
  'HLT-02|TOO-299|自动化|PASS|docs/test/m11-e3.md'
  'UPD-01|TOO-302|自动化 + 人工 macOS|PASS|docs/test/m11-e6.md'
  'UPD-02|TOO-302|自动化 + 发布 E2E|PASS|docs/test/m11-e6.md'
  'UPD-03|TOO-302|发布 E2E|PASS|docs/test/m11-e6.md'
  'PER-01|TOO-299|自动化 + 真实数据|PASS|docs/test/m11-e3.md'
  'PRV-01|TOO-300|自动化 + 真实数据 + 发布 E2E|PASS|docs/test/m11-e4.md'
  'A11-01|TOO-301|人工 macOS + 自动化|EXCLUDED|TOO-301 用户取消'
  'REL-01|TOO-303|自动化 + 发布 E2E|EXCLUDED|TOO-303 用户取消'
)

matrix_rows=$(awk '
  /^## 统一场景矩阵$/ { in_matrix = 1; next }
  in_matrix && /^## / { exit }
  in_matrix && /^\| [A-Z0-9]+-[0-9]+ \|/ { print }
' "$document")

row_count=$(printf '%s\n' "$matrix_rows" | awk 'NF { count++ } END { print count + 0 }')
if [[ "$row_count" -ne "${#matrix_specs[@]}" ]]; then
  echo "M11-004: expected ${#matrix_specs[@]} unique scenario rows, found $row_count" >&2
  failed=1
fi

cell() {
  printf '%s\n' "$1" | awk -F'|' -v index="$2" '
    {
      value = $index
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      print value
    }
  '
}

for spec in "${matrix_specs[@]}"; do
  IFS='|' read -r scenario expected_responsibility expected_execution expected_result_class expected_evidence <<EOF
$spec
EOF

  matches=$(printf '%s\n' "$matrix_rows" | grep -F "| $scenario |" || true)
  match_count=$(printf '%s\n' "$matches" | awk 'NF { count++ } END { print count + 0 }')
  if [[ "$match_count" -ne 1 ]]; then
    echo "M11-005: scenario $scenario must appear exactly once in the matrix; found $match_count" >&2
    failed=1
    continue
  fi

  domain=$(cell "$matches" 3)
  execution=$(cell "$matches" 4)
  precondition=$(cell "$matches" 5)
  action=$(cell "$matches" 6)
  evidence=$(cell "$matches" 7)
  cleanup=$(cell "$matches" 8)
  responsibility=$(cell "$matches" 9)
  severity=$(cell "$matches" 10)
  result=$(cell "$matches" 11)

  for required_cell in domain precondition action evidence cleanup; do
    value=${!required_cell}
    if [[ -z "$value" ]]; then
      echo "M11-006: scenario $scenario has an empty $required_cell cell" >&2
      failed=1
    fi
  done

  if [[ "$execution" != "$expected_execution" ]]; then
    echo "M11-007: scenario $scenario execution type mismatch: expected '$expected_execution', got '$execution'" >&2
    failed=1
  fi
  if [[ "$responsibility" != "$expected_responsibility" ]]; then
    echo "M11-008: scenario $scenario responsibility mismatch: expected '$expected_responsibility', got '$responsibility'" >&2
    failed=1
  fi
  if [[ "$severity" != "blocking" ]]; then
    echo "M11-009: required scenario $scenario must remain blocking" >&2
    failed=1
  fi
  if [[ ! "$result" =~ ^(未执行|PASS（.+）|PARTIAL[[:space:]]PASS（.+）|EXCLUDED（.+）)$ ]]; then
    echo "M11-010: scenario $scenario has an unsupported or evidence-free result: '$result'" >&2
    failed=1
  elif [[ "$result" != "$expected_result_class"'（'* ]]; then
    echo "M11-013: scenario $scenario result class mismatch: expected '$expected_result_class', got '$result'" >&2
    failed=1
  elif [[ "$result" != *"$expected_evidence"* ]]; then
    echo "M11-014: scenario $scenario result does not cite its required evidence: '$expected_evidence'" >&2
    failed=1
  elif [[ "$expected_evidence" == docs/test/* && ! -f "$expected_evidence" ]]; then
    echo "M11-015: scenario $scenario evidence file is missing: '$expected_evidence'" >&2
    failed=1
  fi
done

required_references=(
  docs/test/codex-home-onboarding.md
  docs/test/incremental-index.md
  docs/test/cost-ledger.md
  docs/test/quota-current.md
  docs/test/m7-e7.md
  docs/test/m8-e6.md
  docs/test/m9-e6/README.md
  docs/test/m10-e6.md
  docs/test/m11-e2.md
  docs/test/m11-e3.md
  docs/test/m11-e4.md
  docs/test/m11-e6.md
)

for reference in "${required_references[@]}"; do
  if [[ ! -f "$reference" ]]; then
    echo "M11-011: referenced execution evidence is missing: $reference" >&2
    failed=1
  fi
  if ! grep -Fq -- "$reference" "$document"; then
    echo "M11-012: acceptance matrix does not cite required evidence: $reference" >&2
    failed=1
  fi
done

if [[ "$failed" -ne 0 ]]; then
  exit 1
fi

echo "M11 acceptance matrix contract: PASS"
