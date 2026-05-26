//go:build dev

package main

import "io/fs"

func embeddedFS() fs.FS {
	return nil
}
