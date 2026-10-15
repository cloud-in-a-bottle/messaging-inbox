// Package static holds the inbox's UI. It builds on the messaging apps' shared UI, served under /static/core/.
package static

import "embed"

//go:embed *.html *.js *.css *.svg
var FS embed.FS
