//go:build no_ui

package dashboard

import "embed"

// webFiles stays empty so the binary carries no frontend assets.
var webFiles embed.FS

const dashboardUIEmbedded = false
