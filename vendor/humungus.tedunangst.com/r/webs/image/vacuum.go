//
// Copyright (c) 2019 Ted Unangst <tedu@tedunangst.com>
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

// basic image manipulation (resizing)
package image

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// A returned image in compressed format
type Image struct {
	Data   []byte
	Format string
	Width  int
	Height int
}

// Argument for the Vacuum function
type Params struct {
	LimitSize int // max input dimension in pixels
	MaxWidth  int
	MaxHeight int
	MaxSize   int // max output file size in bytes
	Quality   int // for jpeg output
}

const dirLeft = 1
const dirRight = 2

func fixrotation(img image.Image, dir int) image.Image {
	w, h := img.Bounds().Max.X, img.Bounds().Max.Y
	newimg := image.NewRGBA(image.Rectangle{Max: image.Point{X: h, Y: w}})
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			c := img.At(i, j)
			if dir == dirLeft {
				newimg.Set(j, w-i-1, c)
			} else {
				newimg.Set(h-j-1, i, c)
			}
		}
	}
	return newimg
}

var rotateLeftSigs = [][]byte{
	{0x01, 0x12, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01, 0x00, 0x08},
	{0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x08, 0x00},
}
var rotateRightSigs = [][]byte{
	{0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x06, 0x00},
	{0x01, 0x12, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01, 0x00, 0x06},
}

type bytebuffer struct {
	*bytes.Buffer
}

func (bb bytebuffer) Peek(n int) ([]byte, error) {
	buf := bb.Bytes()
	if n > len(buf) {
		return buf[:len(buf)], io.EOF
	}
	return buf[:n], nil
}

// Read an image and shrink it down to web scale
func VacuumBytes(data []byte, params Params) (*Image, error) {
	r := bytes.NewBuffer(data)
	conf, _, err := image.DecodeConfig(bytebuffer{r})
	if err != nil {
		return nil, err
	}
	limitSize := 16000
	if conf.Width > limitSize || conf.Height > limitSize ||
		(params.LimitSize > 0 && conf.Width*conf.Height > params.LimitSize) {
		return nil, fmt.Errorf("image is too large: x: %d y: %d", conf.Width, conf.Height)
	}
	read := len(data) - r.Len()
	peek := data[:read]
	r = bytes.NewBuffer(data)
	img, format, err := image.Decode(bytebuffer{r})
	if err != nil {
		return nil, err
	}
	return vacuum(img, format, peek, &params)
}

// Read an image and shrink it down to web scale
func Vacuum(reader io.Reader, params Params) (*Image, error) {
	var tmpbuf bytes.Buffer
	tee := io.TeeReader(reader, &tmpbuf)
	conf, _, err := image.DecodeConfig(tee)
	if err != nil {
		return nil, err
	}
	limitSize := 16000
	if conf.Width > limitSize || conf.Height > limitSize ||
		(params.LimitSize > 0 && conf.Width*conf.Height > params.LimitSize) {
		return nil, fmt.Errorf("image is too large: x: %d y: %d", conf.Width, conf.Height)
	}
	peek := tmpbuf.Bytes()
	img, format, err := image.Decode(io.MultiReader(bytes.NewReader(peek), reader))
	if err != nil {
		return nil, err
	}
	return vacuum(img, format, peek, &params)
}

func vacuum(img image.Image, format string, peek []byte, params *Params) (*Image, error) {
	maxh := params.MaxHeight
	maxw := params.MaxWidth
	if maxw == 0 {
		maxw = 16000
	}
	if maxh == 0 {
		maxh = 16000
	}
	if params.MaxSize == 0 {
		params.MaxSize = 512 * 1024
	}

	if format == "jpeg" {
		for _, sig := range rotateLeftSigs {
			if bytes.Contains(peek, sig) {
				img = fixrotation(img, dirLeft)
				break
			}
		}
		for _, sig := range rotateRightSigs {
			if bytes.Contains(peek, sig) {
				img = fixrotation(img, dirRight)
				break
			}
		}
	}

	bounds := img.Bounds()
	if bounds.Max.X > maxw || bounds.Max.Y > maxh {
		if bounds.Max.X > maxw {
			r := float64(maxw) / float64(bounds.Max.X)
			bounds.Max.X = maxw
			bounds.Max.Y = int(float64(bounds.Max.Y) * r)
		}
		if bounds.Max.Y > maxh {
			r := float64(maxh) / float64(bounds.Max.Y)
			bounds.Max.Y = maxh
			bounds.Max.X = int(float64(bounds.Max.X) * r)
		}
		dst := image.NewRGBA(bounds)
		draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
		img = dst
		bounds = img.Bounds()
	}

	quality := params.Quality
	if quality == 0 {
		quality = 80
	}
	var buf bytes.Buffer
	for {
		switch format {
		case "gif":
			format = "png"
			png.Encode(&buf, img)
		case "png":
			png.Encode(&buf, img)
		case "avif":
			fallthrough
		case "heic":
			fallthrough
		case "webp":
			format = "jpeg"
			fallthrough
		case "jpeg":
			jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
		default:
			return nil, fmt.Errorf("can't encode format: %s", format)
		}
		if buf.Len() > params.MaxSize && quality > 30 {
			switch format {
			case "png":
				format = "jpeg"
			case "jpeg":
				quality -= 10
			}
			buf.Reset()
			continue
		}
		break
	}
	rv := &Image{
		Data:   buf.Bytes(),
		Format: format,
		Width:  img.Bounds().Max.X,
		Height: img.Bounds().Max.Y,
	}
	return rv, nil
}
