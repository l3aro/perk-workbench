package uikit

// PrevLabel and NextLabel are the pager button labels on the browse
// button row and the notification history modal.
const (
	PrevLabel = "◀ Prev"
	NextLabel = "Next ▶"
)

// Pager describes a Prev/Next button row: the rendered line, each
// button's content-x span, and whether each button is enabled. Prev is
// pinned to the left edge and Next to the right edge of the row, so
// enabling or disabling a button never moves the other. A disabled button
// renders in the secondary color and ignores clicks; an enabled one
// renders in the primary color and pages on click. The rendered line and
// the click hit-test share this one source of truth.
type Pager struct {
	Line                 string
	Prev, Next           string
	PrevStart, NextStart int
	PrevEnabled          bool
	NextEnabled          bool
}

// NewPager renders the two buttons with their availability styling; the
// caller lays the row out (pin positions and any status text between the
// buttons) and fills Line.
func NewPager(prevEnabled, nextEnabled bool) Pager {
	pager := Pager{
		Prev:        ButtonCancelStyle.Render(PrevLabel),
		Next:        ButtonCancelStyle.Render(NextLabel),
		PrevEnabled: prevEnabled,
		NextEnabled: nextEnabled,
	}
	if pager.PrevEnabled {
		pager.Prev = ButtonSaveStyle.Render(PrevLabel)
	}
	if pager.NextEnabled {
		pager.Next = ButtonSaveStyle.Render(NextLabel)
	}
	return pager
}
