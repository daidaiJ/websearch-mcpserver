//go:build !no_ui

package dashboard

import "embed"

//go:embed web/dist web/logo-*.png
var webFiles embed.FS

const dashboardUIEmbedded = true
