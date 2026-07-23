//go:build !((darwin || linux || windows || freebsd || openbsd || netbsd) && !ios && !android)

package clipboard

func initClipboard() error {
	return ErrUnsupported
}

func writeText(string) {}
