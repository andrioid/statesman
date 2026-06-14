// Command statesman is the codegen + diagram CLI.
//
//	statesman init <name>     bootstrap a runnable machine package
//	statesman stub [dir]       emit user-owned stubs for the unresolved set
//	statesman generate [dir]   (re)generate <id>.machine.gen.go
//	statesman diagram [path]   render machine.json as mermaid or a terminal tree
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/diagram"
	"github.com/andrioid/statesman/internal/codegen"
	"github.com/andrioid/statesman/schema"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "generate":
		err = generate(dirArg(os.Args[2:]))
	case "stub":
		err = stub(dirArg(os.Args[2:]))
	case "diagram":
		err = diagramCmd(os.Args[2:])
	case "init":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "statesman init: missing <name>")
			os.Exit(2)
		}
		err = initMachine(os.Args[2])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "statesman: "+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: statesman <init|stub|generate|diagram> [args]")
	os.Exit(2)
}

func dirArg(args []string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	return "."
}

func loadDef(dir string) (*statesman.Definition, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.machine.json"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no *.machine.json found in %s", dir)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("%s: multiple *.machine.json (one machine per package)", dir)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, err
	}
	def, err := schema.Load(data)
	if err != nil {
		return nil, err
	}
	if want := def.ID + ".machine.json"; filepath.Base(matches[0]) != want {
		return nil, fmt.Errorf("%s: filename must match machine id; rename to %q", matches[0], want)
	}
	return def, nil
}

func generate(dir string) error {
	def, err := loadDef(dir)
	if err != nil {
		return err
	}
	res, err := codegen.Resolve(dir, def)
	if err != nil {
		return err
	}
	if len(res.Unresolved) > 0 {
		mj := filepath.Join(dir, def.ID+".machine.json")
		for _, u := range res.Unresolved {
			fmt.Fprintf(os.Stderr, "%s: unresolved %s %q (run `statesman stub`)\n", mj, u.Kind, u.GoName)
		}
		return fmt.Errorf("%d unresolved name(s)", len(res.Unresolved))
	}
	src, err := codegen.Emit(res, def)
	if err != nil {
		return err
	}
	out := filepath.Join(dir, def.ID+".machine.gen.go")
	if err := os.WriteFile(out, src, 0o644); err != nil {
		return err
	}
	if bytes.Contains(src, []byte("statesman.Unspecified")) {
		fmt.Fprintf(os.Stderr, "%s: warning: statesman.Unspecified survives in generated code; fill the TODO types\n", out)
	}
	for _, w := range codegen.Warnings(res, def) {
		fmt.Fprintf(os.Stderr, "%s: warning: %s\n", filepath.Join(dir, def.ID+".machine.json"), w)
	}
	fmt.Printf("wrote %s\n", out)
	return nil
}

func stub(dir string) error {
	def, err := loadDef(dir)
	if err != nil {
		return err
	}
	res, err := codegen.Resolve(dir, def)
	if err != nil {
		return err
	}
	if len(res.Unresolved) == 0 {
		fmt.Println("nothing to stub: all names resolve")
		return nil
	}
	pkgName := filepath.Base(dir)
	if res.Pkg != nil && res.Pkg.Name != "" {
		pkgName = res.Pkg.Name
	}
	for file, content := range codegen.Stub(res, def) {
		if err := appendToFile(filepath.Join(dir, file), content, pkgName); err != nil {
			return err
		}
		fmt.Printf("stubbed %s\n", filepath.Join(dir, file))
	}
	return nil
}

// appendToFile appends content to an existing file, or creates it with a package
// header and the common imports when absent (best-effort; run goimports after).
func appendToFile(path, content, pkgName string) error {
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		return os.WriteFile(path, append(existing, []byte(content)...), 0o644)
	case os.IsNotExist(err):
		body := []byte(fileHeader(pkgName, content) + content)
		if formatted, ferr := format.Source(body); ferr == nil {
			body = formatted
		}
		return os.WriteFile(path, body, 0o644)
	default:
		return err
	}
}

// fileHeader builds a package clause plus only the imports the stub content
// actually references (run goimports for anything richer).
func fileHeader(pkgName, content string) string {
	var imps []string
	if bytes.Contains([]byte(content), []byte("context.")) {
		imps = append(imps, "\t\"context\"")
	}
	if bytes.Contains([]byte(content), []byte("statesman.")) {
		imps = append(imps, "\t\"github.com/andrioid/statesman\"")
	}
	h := "package " + pkgName + "\n"
	if len(imps) > 0 {
		h += "\nimport (\n" + joinLines(imps) + ")\n"
	}
	return h
}

func joinLines(lines []string) string {
	var b bytes.Buffer
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}
func initMachine(name string) error {
	if err := os.MkdirAll(name, 0o755); err != nil {
		return err
	}
	id := filepath.Base(name)
	mj := filepath.Join(name, id+".machine.json")
	if _, err := os.Stat(mj); os.IsNotExist(err) {
		starter := fmt.Sprintf(`{
  "id": %q,
  "initial": "idle",
  "states": {
    "idle": {"on": {"GO": {"target": "done"}}},
    "done": {"type": "final"}
  }
}
`, id)
		if err := os.WriteFile(mj, []byte(starter), 0o644); err != nil {
			return err
		}
	}
	gengo := filepath.Join(name, "gen.go")
	if _, err := os.Stat(gengo); os.IsNotExist(err) {
		doc := fmt.Sprintf("package %s\n\n//go:generate go tool statesman generate\n", id)
		if err := os.WriteFile(gengo, []byte(doc), 0o644); err != nil {
			return err
		}
	}
	if err := stub(name); err != nil {
		return err
	}
	return generate(name)
}

const watchInterval = 200 * time.Millisecond

func diagramCmd(args []string) error {
	fs := flag.NewFlagSet("diagram", flag.ContinueOnError)
	format := fs.String("format", "", "output format: mermaid|term (default: term on a TTY, else mermaid)")
	outPath := fs.String("o", "", "write output to this file instead of stdout")
	watch := fs.Bool("watch", false, "re-render on every change to the machine.json")
	ascii := fs.Bool("ascii", false, "ASCII-only glyphs (term format)")
	verbose := fs.Bool("verbose", false, "include actions and entry/exit (term format)")
	// Accept the path before or after flags. Go's flag package stops at the
	// first positional, so pull a leading path off before parsing the rest.
	path := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		path = args[0]
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if path == "." && fs.NArg() > 0 {
		path = fs.Arg(0)
	}

	f := *format
	if f == "" {
		f = "mermaid"
		if isTTY(os.Stdout) {
			f = "term"
		}
	}
	if f != "mermaid" && f != "term" {
		return fmt.Errorf("unknown format %q (want mermaid|term)", f)
	}

	render := func(def *statesman.Definition) string {
		if f == "mermaid" {
			return diagram.Mermaid(def)
		}
		var opts []diagram.Option
		if *ascii {
			opts = append(opts, diagram.WithASCII(true))
		}
		if *verbose {
			opts = append(opts, diagram.WithVerbose(true))
		}
		// Color only when drawing to an interactive terminal that allows it.
		if *outPath == "" && isTTY(os.Stdout) && os.Getenv("NO_COLOR") == "" {
			opts = append(opts, diagram.WithColor(true))
		}
		return diagram.Text(def, opts...)
	}

	if *watch {
		return watchDiagram(path, f, *outPath, render, os.Stdout)
	}

	def, _, err := loadDefFromPath(path)
	if err != nil {
		return err
	}
	out := render(def)
	if *outPath != "" {
		if err := os.WriteFile(*outPath, []byte(out), 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", *outPath)
		return nil
	}
	fmt.Print(out)
	return nil
}

// watchDiagram re-renders on every change to the machine.json. The term format
// repaints an alternate screen; the mermaid format rewrites the -o file (or
// stdout) so an editor's mermaid preview updates as you save.
func watchDiagram(path, format, outPath string, render func(*statesman.Definition) string, w io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if format == "term" {
		return watchTerm(ctx, path, render, w)
	}
	return watchMermaid(ctx, path, outPath, render, w)
}

func watchTerm(ctx context.Context, path string, render func(*statesman.Definition) string, w io.Writer) error {
	screen := diagram.NewScreen(w)
	screen.Enter()
	defer screen.Leave()
	var lastGood string
	have := false
	return pollLoop(ctx, path, func(def *statesman.Definition, lerr error) {
		body := ""
		if lerr == nil {
			body = render(def)
		}
		frame, ng, nh := watchFrame(body, lerr, lastGood, have)
		lastGood, have = ng, nh
		screen.Frame(frame + "\n\n" + time.Now().Format("15:04:05") + "\n")
	})
}

func watchMermaid(ctx context.Context, path, outPath string, render func(*statesman.Definition) string, w io.Writer) error {
	return pollLoop(ctx, path, func(def *statesman.Definition, lerr error) {
		if lerr != nil {
			// Keep the last good output file untouched; just report the error.
			fmt.Fprintln(os.Stderr, "statesman diagram: "+lerr.Error())
			return
		}
		out := render(def)
		if outPath == "" {
			fmt.Fprint(w, out)
			return
		}
		if err := os.WriteFile(outPath, []byte(out), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "statesman diagram: "+err.Error())
			return
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", outPath)
	})
}

// pollLoop calls onChange once at startup and again whenever the machine.json's
// mtime changes. mtime polling (vs fsnotify) is zero-dependency and immune to
// the editor atomic-rename save pattern that defeats inode-level file watches.
func pollLoop(ctx context.Context, path string, onChange func(*statesman.Definition, error)) error {
	jsonPath, err := resolveJSONPath(path)
	if err != nil {
		return err
	}
	var lastMod time.Time
	first := true
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if mod := modTime(jsonPath); first || !mod.Equal(lastMod) {
			first = false
			lastMod = mod
			def, _, lerr := loadDefFromPath(path)
			onChange(def, lerr)
		}
		time.Sleep(watchInterval)
	}
}

// watchFrame computes the frame to display after a reload. On success it shows
// the fresh render; on failure it keeps the last valid render and appends the
// error, so an in-progress (invalid) edit never blanks the diagram.
func watchFrame(rendered string, loadErr error, lastGood string, haveGood bool) (frame, newGood string, newHave bool) {
	if loadErr == nil {
		return rendered, rendered, true
	}
	if !haveGood {
		return "⚠ " + loadErr.Error() + "\n\n(waiting for a valid machine.json)", "", false
	}
	return lastGood + "\n⚠ " + loadErr.Error() + "  · showing last valid", lastGood, true
}

func loadDefFromPath(path string) (*statesman.Definition, string, error) {
	jsonPath, err := resolveJSONPath(path)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, "", err
	}
	def, err := schema.Load(data)
	if err != nil {
		return nil, "", err
	}
	return def, jsonPath, nil
}

// resolveJSONPath accepts either the machine.json itself or a directory holding
// exactly one *.machine.json (the one-machine-per-package rule).
func resolveJSONPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return path, nil
	}
	matches, err := filepath.Glob(filepath.Join(path, "*.machine.json"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no *.machine.json found in %s", path)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("%s: multiple *.machine.json (one machine per package)", path)
	}
	return matches[0], nil
}

func modTime(path string) time.Time {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

func isTTY(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
