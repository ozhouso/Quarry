// Package pbo - reads the header of a pbo and hands back the list of files inside
package pbo

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	MethodRaw        = 0x00000000
	MethodCompressed = 0x43707273 // "Cprs"
	methodVersion    = 0x56657273 // "Vers"
)

type Entry struct {
	Name     string // stored with backslashs, we fix that later
	Method   uint32
	OrigSize uint32
	DataSize uint32
	Offset   int64 // where the data actualy starts in the file
}

type Archive struct {
	Path    string
	Size    int64
	ModTime int64
	Props   map[string]string
	Entries []Entry
}

// Prefix gives back the pbo prefix. If theres no prefix in the header we just
// use the filename instead, seems to work fine
func (a *Archive) Prefix() string {
	if p, ok := SafeRel(a.Props["prefix"]); ok {
		return p
	}
	return strings.TrimSuffix(filepath.Base(a.Path), filepath.Ext(a.Path))
}

// Open only reads the header, the file data gets read later when we need it
func Open(path string) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	a := &Archive{Path: path, Size: st.Size(), ModTime: st.ModTime().UnixNano(), Props: map[string]string{}}

	r := bufio.NewReaderSize(f, 1<<18)
	var pos int64
	readCString := func() (string, error) {
		s, err := r.ReadString(0)
		if err != nil {
			return "", err
		}
		pos += int64(len(s))
		return s[:len(s)-1], nil
	}
	var hdr [20]byte
	for i := 0; ; i++ {
		name, err := readCString()
		if err != nil {
			return nil, fmt.Errorf("%s: bad header: %w", path, err)
		}
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return nil, fmt.Errorf("%s: bad header: %w", path, err)
		}
		pos += 20
		e := Entry{
			Name:     name,
			Method:   binary.LittleEndian.Uint32(hdr[0:]),
			OrigSize: binary.LittleEndian.Uint32(hdr[4:]),
			DataSize: binary.LittleEndian.Uint32(hdr[16:]),
		}
		if i == 0 && e.Method == methodVersion {
			for {
				k, err := readCString()
				if err != nil {
					return nil, fmt.Errorf("%s: bad properties: %w", path, err)
				}
				if k == "" {
					break
				}
				v, err := readCString()
				if err != nil {
					return nil, fmt.Errorf("%s: bad properties: %w", path, err)
				}
				a.Props[strings.ToLower(k)] = v
			}
			continue
		}
		if name == "" {
			break
		}
		a.Entries = append(a.Entries, e)
	}
	off := pos
	for i := range a.Entries {
		a.Entries[i].Offset = off
		off += int64(a.Entries[i].DataSize)
	}
	if off > a.Size {
		return nil, errors.New(path + ": entries extend past end of file")
	}
	return a, nil
}

// SafeRel turns a stored name into a normal relative path. Anything with .. in it
// gets thrown out so a bad pbo cant write outside the output folder
func SafeRel(name string) (string, bool) {
	name = strings.ReplaceAll(name, `\`, "/")
	var parts []string
	for _, p := range strings.Split(name, "/") {
		switch p {
		case "", ".":
		case "..":
			return "", false
		default:
			if strings.ContainsAny(p, `:*?"<>|`) {
				return "", false
			}
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return filepath.Join(parts...), true
}
