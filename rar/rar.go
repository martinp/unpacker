package rar

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"

	"github.com/mpolden/sfv"
	"github.com/nwaples/rardecode"
	"github.com/pkg/errors"
)

var rarPartRE = regexp.MustCompile(`\.part0*(\d+)\.rar$`)

type event struct {
	Base string
	Dir  string
	Name string
	sfv  *sfv.SFV
}

type Handler struct {
	mu sync.Mutex
}

func NewHandler() *Handler { return &Handler{} }

func eventFrom(filename string) (event, error) {
	dir := filepath.Dir(filename)
	sfv, err := sfv.Find(dir)
	if err != nil {
		return event{}, err
	}
	rar, err := findFirstRAR(sfv)
	if err != nil {
		return event{}, err
	}
	return event{
		sfv:  sfv,
		Base: filepath.Base(rar),
		Dir:  dir,
		Name: rar,
	}, nil
}

func isRAR(name string) bool { return filepath.Ext(name) == ".rar" }

func isFirstRAR(name string) bool {
	m := rarPartRE.FindStringSubmatch(name)
	if len(m) == 2 {
		return m[1] == "1"
	}
	return isRAR(name)
}

func findFirstRAR(s *sfv.SFV) (string, error) {
	for _, c := range s.Checksums {
		if isFirstRAR(c.Path) {
			return c.Path, nil
		}
	}
	return "", errors.Errorf("no rar found in %s", s.Path)
}

func chtimes(name string, header *rardecode.FileHeader) error {
	if header.ModificationTime.IsZero() {
		return nil
	}
	return os.Chtimes(name, header.ModificationTime, header.ModificationTime)
}

func unpack(filename string) error {
	r, err := rardecode.OpenReader(filename, "")
	if err != nil {
		return errors.Wrapf(err, "failed to open %s", filename)
	}
	dir := filepath.Dir(filename)
	for {
		header, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Join(dir, header.Name)
		// If entry is a directory, create it and set correct ctime
		if header.IsDir {
			if err := os.MkdirAll(name, 0755); err != nil {
				return err
			}
			if err := chtimes(name, header); err != nil {
				return err
			}
			continue
		}
		// Files can come before their containing folders, ensure that parent is created
		parent := filepath.Dir(name)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return err
		}
		if err := chtimes(parent, header); err != nil {
			return err
		}
		// Unpack file
		f, err := os.Create(name)
		if err != nil {
			return errors.Wrapf(err, "failed to create file: %s", name)
		}
		if _, err = io.Copy(f, r); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		// Set correct ctime of unpacked file
		if err := chtimes(name, header); err != nil {
			return err
		}
		// Unpack recursively if unpacked file is also a RAR
		if isRAR(name) {
			if err := unpack(name); err != nil {
				return err
			}
		}
	}
	return nil
}

func remove(sfv *sfv.SFV) error {
	for _, c := range sfv.Checksums {
		if err := os.Remove(c.Path); err != nil {
			return err
		}
	}
	return os.Remove(sfv.Path)
}

func fileCount(sfv *sfv.SFV) (int, int) {
	exists := 0
	for _, c := range sfv.Checksums {
		if c.IsExist() {
			exists++
		}
	}
	return exists, len(sfv.Checksums)
}

func verify(sfv *sfv.SFV) error {
	for _, c := range sfv.Checksums {
		ok, err := c.Verify()
		if err != nil {
			return err
		}
		if !ok {
			return errors.Errorf("%s: failed checksum: %s", sfv.Path, c.Filename)
		}
	}
	return nil
}

func cmdFrom(tmpl string, ev event) (*exec.Cmd, error) {
	t, err := template.New("cmd").Parse(tmpl)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, ev); err != nil {
		return nil, err
	}
	argv := strings.Split(b.String(), " ")
	if len(argv) == 0 {
		return nil, errors.New("template compiled to empty command")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = ev.Dir
	return cmd, nil
}

func runCmd(command string, e event) error {
	if command == "" {
		return nil
	}
	cmd, err := cmdFrom(command, e)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errors.Wrapf(err, "stderr: %q", stderr.String())
	}
	return nil
}

func (h *Handler) Handle(name, postCommand string, removeRARs bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	ev, err := eventFrom(name)
	if err != nil {
		return err
	}
	if exists, total := fileCount(ev.sfv); exists != total {
		return errors.Errorf("incomplete: %s: %d/%d files", ev.Dir, exists, total)
	}
	if err := verify(ev.sfv); err != nil {
		return errors.Wrapf(err, "verification failed: %s", ev.Dir)
	}
	if err := unpack(ev.Name); err != nil {
		return errors.Wrapf(err, "unpacking failed: %s", ev.Dir)
	}
	if removeRARs {
		if err := remove(ev.sfv); err != nil {
			return errors.Wrapf(err, "removal failed: %s", ev.Dir)
		}
	}
	if err := runCmd(postCommand, ev); err != nil {
		return errors.Wrapf(err, "post-process command failed: %s", ev.Dir)
	}
	return nil
}
