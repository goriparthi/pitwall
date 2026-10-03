// Package web embeds the dashboard UI served by the HTTP server and captured by the display bridge.
package web

import "embed"

//go:embed index.html css js
var FS embed.FS
