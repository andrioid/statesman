// Command statesman is the codegen CLI: one binary, three verbs (decision 49).
//
//	statesman init <name>    bootstrap a runnable machine package
//	statesman stub [dir]      emit user-owned stubs for the unresolved set
//	statesman generate [dir]  (re)generate machine_gen.go
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"

	"github.com/andrioid/statesman"
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
	fmt.Fprintln(os.Stderr, "usage: statesman <init|stub|generate> [args]")
	os.Exit(2)
}

func dirArg(args []string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	return "."
}

func loadDef(dir string) (*statesman.Definition, error) {
	data, err := os.ReadFile(filepath.Join(dir, "machine.json"))
	if err != nil {
		return nil, err
	}
	return schema.Load(data)
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
		mj := filepath.Join(dir, "machine.json")
		for _, u := range res.Unresolved {
			fmt.Fprintf(os.Stderr, "%s: unresolved %s %q (run `statesman stub`)\n", mj, u.Kind, u.GoName)
		}
		return fmt.Errorf("%d unresolved name(s)", len(res.Unresolved))
	}
	src, err := codegen.Emit(res, def)
	if err != nil {
		return err
	}
	out := filepath.Join(dir, "machine_gen.go")
	if err := os.WriteFile(out, src, 0o644); err != nil {
		return err
	}
	if bytes.Contains(src, []byte("statesman.Unspecified")) {
		fmt.Fprintf(os.Stderr, "%s: warning: statesman.Unspecified survives in generated code; fill the TODO types\n", out)
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
	mj := filepath.Join(name, "machine.json")
	if _, err := os.Stat(mj); os.IsNotExist(err) {
		starter := fmt.Sprintf(`{
  "id": %q,
  "initial": "idle",
  "states": {
    "idle": {"on": {"GO": {"target": "done"}}},
    "done": {"type": "final"}
  }
}
`, name)
		if err := os.WriteFile(mj, []byte(starter), 0o644); err != nil {
			return err
		}
	}
	gengo := filepath.Join(name, "gen.go")
	if _, err := os.Stat(gengo); os.IsNotExist(err) {
		doc := fmt.Sprintf("package %s\n\n//go:generate statesman generate\n", name)
		if err := os.WriteFile(gengo, []byte(doc), 0o644); err != nil {
			return err
		}
	}
	if err := stub(name); err != nil {
		return err
	}
	return generate(name)
}
