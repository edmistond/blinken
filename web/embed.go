// Package web holds the browser review UI, compiled into the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html static
var assets embed.FS

// FS returns the embedded assets rooted at this package directory.
func FS() fs.FS { return assets }

// Index returns the raw index.html template.
func Index() ([]byte, error) { return assets.ReadFile("index.html") }
