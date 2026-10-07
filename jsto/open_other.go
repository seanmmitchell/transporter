//go:build !unix

package jsto

import "os"

// openForRead opens path for reading.
func openForRead(path string) (*os.File, error) {
	return os.Open(path)
}
