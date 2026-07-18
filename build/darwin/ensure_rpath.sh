#!/usr/bin/env bash

set -euo pipefail

[[ $# -eq 2 ]] || {
    echo "usage: $0 <mach-o> <rpath>" >&2
    exit 64
}

binary=$1
rpath=$2
[[ -x "$binary" ]] || {
    echo "ensure_rpath: executable does not exist: $binary" >&2
    exit 1
}

if otool -l "$binary" | awk -v expected="$rpath" '
    $1 == "cmd" && $2 == "LC_RPATH" { in_rpath=1; next }
    in_rpath && $1 == "path" && $2 == expected { found=1 }
    in_rpath && $1 == "cmd" { in_rpath=0 }
    END { exit !found }
'; then
    printf 'rpath already present: %s\n' "$rpath"
    exit 0
fi

install_name_tool -add_rpath "$rpath" "$binary"
printf 'added rpath: %s\n' "$rpath"
