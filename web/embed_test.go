package web

import (
	"io/fs"
	"testing"
)

func TestRequiredAssetsPresent(t *testing.T) {
	for _, name := range []string{"index.html", "static/app.js", "static/style.css"} {
		if _, err := fs.Stat(FS(), name); err != nil {
			t.Errorf("missing embedded asset %s: %v", name, err)
		}
	}
}
