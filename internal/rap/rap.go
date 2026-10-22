// Package rap - turns binarised (raP) configs back into readable text
package rap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"strings"
)

var magic = []byte{0, 'r', 'a', 'P'}

var ErrFormat = errors.New("rap: malformed data")

// Is checks for the raP magic at the start
func Is(data []byte) bool { return bytes.HasPrefix(data, magic) }

type decoder struct {
	b   []byte
	pos int
	out *bytes.Buffer
}

// Decode converts the binary config to text
func Decode(data []byte) (out []byte, err error) {
	if !Is(data) || len(data) < 16 {
		return nil, ErrFormat
	}
	d := &decoder{b: data, pos: 16, out: bytes.NewBuffer(make([]byte, 0, len(data)*3))}
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, ErrFormat
		}
	}()
	d.body(0, true)
	return d.out.Bytes(), nil
}

func (d *decoder) fail() { panic(ErrFormat) }

func (d *decoder) byte() byte {
	if d.pos >= len(d.b) {
		d.fail()
	}
	c := d.b[d.pos]
	d.pos++
	return c
}

func (d *decoder) u32() uint32 {
	if d.pos+4 > len(d.b) {
		d.fail()
	}
	v := binary.LittleEndian.Uint32(d.b[d.pos:])
	d.pos += 4
	return v
}

func (d *decoder) cint() int {
	v := 0
	for shift := 0; ; shift += 7 {
		c := d.byte()
		v |= int(c&0x7F) << shift
		if c&0x80 == 0 {
			return v
		}
		if shift > 28 {
			d.fail()
		}
	}
}

func (d *decoder) cstr() string {
	i := bytes.IndexByte(d.b[d.pos:], 0)
	if i < 0 {
		d.fail()
	}
	s := string(d.b[d.pos : d.pos+i])
	d.pos += i + 1
	return s
}

func (d *decoder) indent(n int) {
	for i := 0; i < n; i++ {
		d.out.WriteString("    ")
	}
}

// body reads a class at the current spot: parent name, then count, then the entries
func (d *decoder) body(depth int, root bool) {
	_ = d.cstr() // root has no parent so we just skip it
	n := d.cint()
	for i := 0; i < n; i++ {
		d.entry(depth)
	}
}

func (d *decoder) entry(depth int) {
	switch t := d.byte(); t {
	case 0: // class, body lives somewhere else in the file
		name := d.cstr()
		off := int(d.u32())
		if off < 0 || off >= len(d.b) {
			d.fail()
		}
		save := d.pos
		d.pos = off
		parent := d.cstr()
		d.indent(depth)
		d.out.WriteString("class " + name)
		if parent != "" {
			d.out.WriteString(": " + parent)
		}
		d.out.WriteString("\n")
		d.indent(depth)
		d.out.WriteString("{\n")
		n := d.cint()
		for i := 0; i < n; i++ {
			d.entry(depth + 1)
		}
		d.indent(depth)
		d.out.WriteString("};\n")
		d.pos = save
	case 1: // single value
		sub := d.byte()
		name := d.cstr()
		d.indent(depth)
		d.out.WriteString(name + " = ")
		d.scalar(sub)
		d.out.WriteString(";\n")
	case 2, 5: // array, or the += kind
		appendOp := false
		if t == 5 {
			appendOp = d.u32() != 0
		}
		name := d.cstr()
		d.indent(depth)
		d.out.WriteString(name)
		if appendOp {
			d.out.WriteString("[] += ")
		} else {
			d.out.WriteString("[] = ")
		}
		d.array()
		d.out.WriteString(";\n")
	case 3:
		d.indent(depth)
		d.out.WriteString("class " + d.cstr() + ";\n")
	case 4:
		d.indent(depth)
		d.out.WriteString("delete " + d.cstr() + ";\n")
	default:
		d.fail()
	}
}

func (d *decoder) scalar(sub byte) {
	switch sub {
	case 0:
		d.quoted(d.cstr())
	case 1:
		d.float(math.Float32frombits(d.u32()))
	case 2:
		d.out.WriteString(strconv.Itoa(int(int32(d.u32()))))
	default:
		d.fail()
	}
}

func (d *decoder) float(f float32) {
	d.out.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
}

func (d *decoder) quoted(s string) {
	d.out.WriteByte('"')
	d.out.WriteString(strings.ReplaceAll(s, `"`, `""`))
	d.out.WriteByte('"')
}

func (d *decoder) array() {
	n := d.cint()
	d.out.WriteByte('{')
	for i := 0; i < n; i++ {
		if i > 0 {
			d.out.WriteString(", ")
		}
		switch t := d.byte(); t {
		case 0:
			d.quoted(d.cstr())
		case 1:
			d.float(math.Float32frombits(d.u32()))
		case 2:
			d.out.WriteString(strconv.Itoa(int(int32(d.u32()))))
		case 3:
			d.array()
		case 4:
			d.out.WriteString(d.cstr())
		default:
			d.fail()
		}
	}
	d.out.WriteByte('}')
}
