package diagram

import (
	"fmt"
	"io"
)

// Screen drives a terminal alternate-screen buffer for full-frame repaints. It
// is shared by the CLI authoring watch and by Live: both clear and redraw the
// whole diagram on each change rather than scrolling.
type Screen struct {
	w io.Writer
}

// NewScreen wraps w (typically os.Stdout) for alternate-screen repainting.
func NewScreen(w io.Writer) *Screen { return &Screen{w: w} }

// Enter switches to the alternate screen buffer and hides the cursor.
func (s *Screen) Enter() { fmt.Fprint(s.w, "\x1b[?1049h\x1b[?25l") }

// Leave restores the cursor and the primary screen buffer.
func (s *Screen) Leave() { fmt.Fprint(s.w, "\x1b[?25h\x1b[?1049l") }

// Frame clears the screen and draws body from the top-left.
func (s *Screen) Frame(body string) {
	fmt.Fprint(s.w, "\x1b[2J\x1b[H")
	fmt.Fprint(s.w, body)
}
