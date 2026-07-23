//go:build (darwin || linux || windows || freebsd || openbsd || netbsd) && !ios && !android

package clipboard

import "golang.design/x/clipboard"

func initClipboard() error {
	return clipboard.Init()
}

func writeText(text string) {
	clipboard.Write(clipboard.FmtText, []byte(text))
}
