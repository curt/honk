//
// Copyright (c) 2025 Ted Unangst <tedu@tedunangst.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package lazif

/*
#include <stdlib.h>
#include <string.h>

#include "lazif.h"
*/
import "C"
import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"runtime"
	"sync"
	"unsafe"
)

type Status int

const AVIF Status = 1
const HEIF Status = 2

var loaded Status

func (s Status) HasAny() bool {
	return s != 0
}
func (s Status) HasAVIF() bool {
	return s&AVIF == AVIF
}
func (s Status) HasHEIF() bool {
	return s&HEIF == HEIF
}

func load() {
	loaded = Status(C.lazifLoad())
}

var once sync.Once

func Load() Status {
	once.Do(load)
	return loaded
}

func Register(stat Status) {
	if stat.HasAVIF() {
		image.RegisterFormat("avif", "????ftypavif", Decode, DecodeConfig)
	}
	if stat.HasHEIF() {
		image.RegisterFormat("heic", "????ftypheic", Decode, DecodeConfig)
	}
}

func EncodeJPEG(data []byte) []byte {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return EncodeImage(img)
}
func EncodeImage(img image.Image) []byte {
	if !loaded.HasAVIF() {
		return nil
	}

	jpg, ok := img.(*image.YCbCr)
	if !ok {
		return nil
	}

	var args C.struct_lazifArgs

	switch jpg.SubsampleRatio {
	case image.YCbCrSubsampleRatio420:
		args.format = C.YUV420
	case image.YCbCrSubsampleRatio444:
		args.format = C.YUV444
	default:
		return nil
	}

	args.width = C.uint(jpg.Rect.Max.X)
	args.height = C.uint(jpg.Rect.Max.Y)

	var pinner runtime.Pinner
	defer pinner.Unpin()

	args.planes[0] = (*C.uchar)(&jpg.Y[0])
	pinner.Pin(args.planes[0])
	args.strides[0] = C.uint(jpg.YStride)
	args.planes[1] = (*C.uchar)(&jpg.Cb[0])
	pinner.Pin(args.planes[1])
	args.strides[1] = C.uint(jpg.CStride)
	args.planes[2] = (*C.uchar)(&jpg.Cr[0])
	pinner.Pin(args.planes[2])
	args.strides[2] = C.uint(jpg.CStride)

	rv := C.lazifEncode(&args)
	if rv != 0 {
		return nil
	}
	defer C.lazifFree(&args)
	res := make([]byte, int(args.datalen))
	C.memcpy(unsafe.Pointer(&res[0]), unsafe.Pointer(args.data), args.datalen)

	return res
}

func DecodeBytes(data []byte) (image.Image, error) {
	if !loaded.HasAny() {
		return nil, fmt.Errorf("can't load libavif")
	}
	var pinner runtime.Pinner
	defer pinner.Unpin()

	var args C.struct_lazifArgs
	args.data = (*C.uchar)(&data[0])
	args.datalen = C.size_t(len(data))
	pinner.Pin(args.data)

	rv := C.lazifDecode(&args)
	args.data = nil
	if rv != 0 {
		return nil, fmt.Errorf("can't decode avif: %s", C.GoString(&args.mesg[0]))
	}
	w := int(args.width)
	h := int(args.height)
	r := image.Rect(0, 0, w, h)
	var ratio image.YCbCrSubsampleRatio
	switch args.format {
	case C.YUV420:
		ratio = image.YCbCrSubsampleRatio420
	case C.YUV444:
		ratio = image.YCbCrSubsampleRatio444
	default:
		C.lazifFree(&args)
		return nil, fmt.Errorf("unsupported YUV ratio: %d", args.format)
	}
	img := image.NewYCbCr(r, ratio)
	runtime.SetFinalizer(img, func(*image.YCbCr) {
		C.lazifFree(&args)
	})
	img.Y = unsafe.Slice((*byte)(args.planes[0]), h*int(args.strides[0]))
	img.YStride = int(args.strides[0])
	img.Cb = unsafe.Slice((*byte)(args.planes[1]), h*int(args.strides[1]))
	img.Cr = unsafe.Slice((*byte)(args.planes[2]), h*int(args.strides[2]))
	img.CStride = int(args.strides[1])

	return img, nil
}

type bytebuffer interface {
	Bytes() []byte
}

func bytesFromReader(r io.Reader) []byte {
	var data []byte
	if bb, ok := r.(bytebuffer); ok {
		data = bb.Bytes()
	} else {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		data = buf.Bytes()
	}
	return data
}

func Decode(r io.Reader) (image.Image, error) {
	return DecodeBytes(bytesFromReader(r))
}

func ConfigBytes(data []byte) (image.Config, error) {
	var config image.Config
	if !loaded.HasAny() {
		return config, fmt.Errorf("can't load libavif")
	}
	var pinner runtime.Pinner
	defer pinner.Unpin()

	var args C.struct_lazifArgs
	args.data = (*C.uchar)(&data[0])
	args.datalen = C.size_t(len(data))
	pinner.Pin(args.data)

	rv := C.lazifConfig(&args)
	args.data = nil
	if rv != 0 {
		return config, fmt.Errorf("can't decode avif: %s", C.GoString(&args.mesg[0]))
	}
	config.ColorModel = color.YCbCrModel
	config.Width = int(args.width)
	config.Height = int(args.height)
	return config, nil
}

func DecodeConfig(r io.Reader) (image.Config, error) {
	return ConfigBytes(bytesFromReader(r))
}
