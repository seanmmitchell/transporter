//go:build !unix

package jsto

import "os"

// openForRead opens path for reading.
func openForRead(path string) (*os.File, error) {
	return os.Open(path)
}

// setBlocking is a no-op: openForRead does not change blocking mode here.
func setBlocking(*os.File) error {
	return nil
}
