// Package diagram renders a statesman machine Definition as a Mermaid
// stateDiagram-v2 string (for docs) or as a Unicode/ANSI outline tree (for the
// terminal). Both outputs are driven by a single walk of the same
// *statesman.Definition; the terminal renderer additionally accepts a live
// snapshot's active states for an overlay (see Live).
//
// The package emits text only: turning Mermaid into pixels is left to whatever
// already renders Mermaid (an editor preview, GitHub, or a native renderer like
// mmdr). statesman takes no rendering dependency.
package diagram
