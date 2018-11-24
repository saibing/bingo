package sys

import "runtime"

const windowsOS = "windows"

func IsWindows() bool {
	return runtime.GOOS == windowsOS
}

