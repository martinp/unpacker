package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

type Path struct {
	Name          string
	MaxDepth      int
	SkipHidden    bool
	Patterns      []string
	Remove        bool
	ArchiveExt    string
	UnpackCommand string
}

type CommandValues struct {
	Name string
	Dir  string
	Base string
}

func PathDepth(name string) int {
	name = filepath.Clean(name)
	return strings.Count(name, string(os.PathSeparator))
}

func (p *Path) Match(name string) (bool, error) {
	for _, pattern := range p.Patterns {
		matched, err := filepath.Match(pattern, name)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func (p *Path) ArchiveExtWithDot() string {
	if strings.HasPrefix(p.ArchiveExt, ".") {
		return p.ArchiveExt
	}
	return "." + p.ArchiveExt
}

func (p *Path) NewUnpackCommand(v CommandValues) (*exec.Cmd, error) {
	t := template.Must(template.New("cmd").Parse(p.UnpackCommand))
	var b bytes.Buffer
	if err := t.Execute(&b, v); err != nil {
		return nil, err
	}
	argv := strings.Split(b.String(), " ")
	if len(argv) == 0 {
		return nil, fmt.Errorf("template compiled to empty command")
	}
	cmd := exec.Command(argv[0])
	cmd.Dir = v.Dir
	if len(argv) > 1 {
		cmd.Args = argv[1:]
	}
	return cmd, nil
}