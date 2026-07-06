//go:build windows

package atomicfile

import "golang.org/x/sys/windows"

func ReplaceFileAtomic(source string, destination string) error {
	from, fromErr := windows.UTF16PtrFromString(source)
	if fromErr != nil {
		return fromErr
	}
	to, toErr := windows.UTF16PtrFromString(destination)
	if toErr != nil {
		return toErr
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
