package web

import "embed"

// Assets contains the production web client built by Vite.
//
//go:embed dist
var Assets embed.FS
