//go:build !windows

package atomicfile

import "os"

func ReplaceFileAtomic(source string, destination string) error {
	return os.Rename(source, destination)
}
