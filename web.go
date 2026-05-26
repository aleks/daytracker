//go:build !dev

package daytracker

import (
	"embed"
	"io/fs"
	"log"
)

//go:embed all:web/dist
var webDist embed.FS

func WebFS() fs.FS {
	sub, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		log.Fatalf("embed: %v", err)
	}
	return sub
}
