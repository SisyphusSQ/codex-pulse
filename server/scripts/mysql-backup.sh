#!/usr/bin/env bash
# 只处理受信任的自有中心备份；不接受用户上传 SQL。
set -euo pipefail
umask 077
if [[ $# != 4 || ( "$1" != backup && "$1" != restore ) ]]; then
  echo '用法: mysql-backup.sh backup|restore PRIVATE_CLIENT_CNF DATABASE BACKUP_DIRECTORY' >&2; exit 2
fi
action=$1; options=$2; database=$3; directory=$4
[[ "$database" =~ ^[A-Za-z0-9_]+$ && "$database" != mysql && "$database" != sys && "$database" != performance_schema && "$database" != information_schema ]] || { echo '需要专用中心数据库名' >&2; exit 2; }
[[ -f "$options" && ! -L "$options" ]] || { echo '需要私有客户端配置' >&2; exit 2; }
if [[ $(uname -s) == Darwin ]]; then mode=$(stat -f '%Lp' "$options"); else mode=$(stat -c '%a' "$options"); fi
[[ "$mode" == 600 ]] || { echo '客户端配置必须为 0600' >&2; exit 2; }
if command -v timeout >/dev/null; then deadline=timeout; elif command -v gtimeout >/dev/null; then deadline=gtimeout; else echo '需要 GNU timeout（macOS: coreutils/gtimeout）' >&2; exit 2; fi
command -v mysql >/dev/null || { echo '需要 MySQL 8.4 客户端' >&2; exit 2; }
if [[ "$action" == backup ]]; then
  command -v mysqldump >/dev/null || { echo '需要 mysqldump' >&2; exit 2; }
  mkdir -m 700 "$directory" # 拒绝覆盖；父目录由操作者准备。
  if ! "$deadline" 600 mysqldump --defaults-file="$options" --no-login-paths --single-transaction --quick --hex-blob --no-tablespaces --set-gtid-purged=OFF --skip-lock-tables --connect-timeout=10 "$database" > "$directory/center.sql" 2> "$directory/operation.log"; then
    echo '备份失败；该目录未完成，检查私有 operation.log' >&2; exit 1
  fi
  (cd "$directory" && shasum -a 256 center.sql > SHA256SUMS)
  echo '备份完成；目录包含账号元数据与凭证摘要，须私有保存'
else
  [[ -d "$directory" && ! -L "$directory" && -f "$directory/center.sql" && ! -L "$directory/center.sql" && -f "$directory/SHA256SUMS" && ! -L "$directory/SHA256SUMS" ]] || { echo '备份不完整' >&2; exit 2; }
  for private_path in "$directory" "$directory/center.sql" "$directory/SHA256SUMS"; do
    if [[ $(uname -s) == Darwin ]]; then mode=$(stat -f '%Lp' "$private_path"); else mode=$(stat -c '%a' "$private_path"); fi
    expected=600; [[ "$private_path" == "$directory" ]] && expected=700
    [[ "$mode" == "$expected" ]] || { echo '备份目录/文件须为 0700/0600' >&2; exit 2; }
  done
  [[ $(cat "$directory/SHA256SUMS") =~ ^[a-f0-9]{64}[[:space:]]+center.sql$ ]] || { echo '摘要格式错误' >&2; exit 2; }
  (cd "$directory" && shasum -a 256 -c SHA256SUMS >/dev/null)
  # 参数库名为固定白名单字符；状态变更不经未验证输入拼接 SQL。
  count=$("$deadline" 30 mysql --defaults-file="$options" --no-login-paths --batch --skip-column-names --connect-timeout=10 "$database" -e 'SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()')
  [[ "$count" == 0 ]] || { echo '恢复只允许专用空数据库；拒绝覆盖现有表' >&2; exit 2; }
  if ! "$deadline" 600 mysql --defaults-file="$options" --no-login-paths --batch --binary-mode --local-infile=0 --connect-timeout=10 "$database" < "$directory/center.sql" 2> "$directory/restore.log"; then
    echo '恢复失败；保留新库用于诊断，禁止切换服务，检查私有 restore.log' >&2; exit 1
  fi
  "$deadline" 30 mysql --defaults-file="$options" --no-login-paths --batch --binary-mode --local-infile=0 --connect-timeout=10 "$database" -e 'START TRANSACTION; SET @restore_ms=CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS UNSIGNED); UPDATE pulse_clients SET revoked_at_ms=@restore_ms WHERE revoked_at_ms IS NULL; UPDATE pulse_pairings SET consumed_at_ms=@restore_ms WHERE consumed_at_ms IS NULL; COMMIT;' 2> "$directory/access-reset.log"
  echo '已恢复且旧授权已撤销；必须使用新库配置运行 db check，并重新配对后再切换入口'
fi
