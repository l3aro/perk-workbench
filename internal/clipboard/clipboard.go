// Package clipboard provides cross-platform clipboard access behind build-tag
// guards so that unsupported platforms compile without platform-specific
// dependencies.
package clipboard

import "errors"

var ErrUnsupported = errors.New("clipboard operations are not supported on this platform")

// Init initializes the native clipboard. On unsupported platforms it returns
// ErrUnsupported; callers may still use terminal OSC 52 clipboard support.
func Init() error {
	return initClipboard()
}

// WriteText writes plain text to the native system clipboard. On unsupported
// platforms it is a no-op.
func WriteText(text string) {
	writeText(text)
}
