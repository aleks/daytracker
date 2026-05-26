//go:build dev

package daytracker

import "io/fs"

func WebFS() fs.FS {
	return nil
}
