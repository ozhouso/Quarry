// Package lzss - unpacks the compressed files inside a pbo (the "Cprs" ones)
package lzss

import (
	"encoding/binary"
	"errors"
)

var (
	ErrTruncated = errors.New("lzss: truncated input")
	ErrChecksum  = errors.New("lzss: checksum mismatch")
)

// Decode unpacks src into dst. dst has to be the full uncompressed size already.
// If theres a 4 byte checksum on the end we check it too
func Decode(src, dst []byte) error {
	si, di := 0, 0
	n := len(dst)
	for di < n {
		if si >= len(src) {
			return ErrTruncated
		}
		flags := src[si]
		si++
		for bit := 0; bit < 8 && di < n; bit++ {
			if flags&1 != 0 {
				if si >= len(src) {
					return ErrTruncated
				}
				dst[di] = src[si]
				si++
				di++
			} else {
				if si+1 >= len(src) {
					return ErrTruncated
				}
				b1, b2 := int(src[si]), int(src[si+1])
				si += 2
				dist := b1 | (b2&0xF0)<<4
				cnt := (b2 & 0x0F) + 3
				if cnt > n-di {
					cnt = n - di
				}
				from := di - dist
				if from < 0 {
					// pointing before the start of the output just means spaces, dont ask me why
					pad := -from
					if pad > cnt {
						pad = cnt
					}
					for i := 0; i < pad; i++ {
						dst[di+i] = 0x20
					}
					di += pad
					cnt -= pad
					from = 0
				}
				if dist >= cnt {
					copy(dst[di:di+cnt], dst[from:from+cnt])
					di += cnt
				} else {
					for i := 0; i < cnt; i++ {
						dst[di] = dst[from+i]
						di++
					}
				}
			}
			flags >>= 1
		}
	}
	if len(src)-si >= 4 {
		var sum uint32
		for _, b := range dst {
			sum += uint32(b)
		}
		if sum != binary.LittleEndian.Uint32(src[si:]) {
			return ErrChecksum
		}
	}
	return nil
}
