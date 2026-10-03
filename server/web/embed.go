// Package web 保存与 Server 一起构建的只读 Web 资源。
package web

import "embed"

// Files 由 make web-build 生成；每个 Server 二进制携带同一版本的页面与 assets。
//
//go:embed dist/index.html dist/assets
var Files embed.FS
