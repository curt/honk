/*
 * Copyright (c) 2025 Ted Unangst <tedu@tedunangst.com>
 *
 * Permission to use, copy, modify, and distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <dlfcn.h>

#include "lazif.h"

struct frame {
	uint32_t width;
	uint32_t height;
	uint32_t depth;

	int yuvFormat;
	int yuvRange;
	int yuvChromaSamplePosition;
	uint8_t * yuvPlanes[3];
	uint32_t yuvRowBytes[3];
	int imageOwnsYUVPlanes;

	uint8_t * alphaPlane;
	uint32_t alphaRowBytes;
	int imageOwnsAlphaPlane;
	int alphaPremultiplied;
};

struct encoder {
	int codecChoice;
	int maxThreads;
	int speed;
	int keyframeInterval;
	uint64_t timescale;
	int repetitionCount;
	uint32_t extraLayerCount;
	int quality;
	int qualityAlpha;
	int minQuantizer;
	int maxQuantizer;
	int minQuantizerAlpha;
	int maxQuantizerAlpha;
};

struct decoder {
	int codecChoice;
	int maxThreads;
	int requestedSource;
	int allowProgressive;
	int allowIncremental;
	int ignoreExif;
	int ignoreXMP;
	uint32_t imageSizeLimit;
	uint32_t imageDimensionLimit;
	uint32_t imageCountLimit;
	int strictFlags;
	struct frame * image;

	int imageIndex;
	int imageCount;
};


struct rwdata {
	unsigned char *data;
	size_t size;
};

static struct frame *(*imgCreate)(unsigned int, unsigned int, int, int);
static struct encoder *(*encCreate)(void);
static int (*encWrite)(struct encoder *, struct frame *, struct rwdata *);
static void (*encDestroy)(struct encoder *);
static void (*imgDestroy)(struct frame *);
static void (*dataFree)(struct rwdata *);

static struct decoder *(*decCreate)(void);
static void (*decDestroy)(struct decoder *);
static int (*setMemory)(struct decoder *, const uint8_t *, size_t);
static int (*decParse)(struct decoder *);
static int (*nextImage)(struct decoder *);

struct herr {
	int code;
	int subcode;
	const char *mesg;
};

struct hctx;
struct hndl;
struct himg;

static struct hctx *(*hctxAlloc)(void);
static void *(*hctxFree)(struct hctx *);
static struct herr (*hctxMemory)(struct hctx *, const void*, size_t, void *);
static struct herr (*hctxHandle)(struct hctx *, struct hndl **);
static struct herr (*hndlDecode)(struct hndl *, struct himg **, int, int, void *);
static int (*hndlWidth)(struct hndl *);
static int (*hndlHeight)(struct hndl *);
static int (*himgChroma)(struct himg *);
static int (*himgWidth)(struct himg *);
static int (*himgHeight)(struct himg *);
static unsigned char *(*himgPlane)(struct himg *, int, int *);

int
lazifLoad(void)
{
	int rv = 0;
	do {
#ifdef __APPLE__
		const char *libname = "/opt/homebrew/lib/libavif.dylib";
#else
		const char *libname = "libavif.so";
#endif
		void *lib = dlopen(libname, RTLD_LAZY);
		if (!lib)
			break;

		if (!(imgCreate = dlsym(lib, "avifImageCreate")))
			break;;
		if (!(encCreate = dlsym(lib, "avifEncoderCreate")))
			break;
		if (!(encWrite = dlsym(lib, "avifEncoderWrite")))
			break;
		if (!(encDestroy = dlsym(lib, "avifEncoderDestroy")))
			break;
		if (!(imgDestroy = dlsym(lib, "avifImageDestroy")))
			break;
		if (!(dataFree = dlsym(lib, "avifRWDataFree")))
			break;

		if (!(decDestroy = dlsym(lib, "avifDecoderDestroy")))
			break;
		if (!(setMemory = dlsym(lib, "avifDecoderSetIOMemory")))
			break;
		if (!(decParse = dlsym(lib, "avifDecoderParse")))
			break;
		if (!(nextImage = dlsym(lib, "avifDecoderNextImage")))
			break;
		if (!(decCreate = dlsym(lib, "avifDecoderCreate")))
			break;
		rv |= 1;
	} while (0);

	do {
#ifdef __APPLE__
		const char *libname = "/opt/homebrew/lib/libheif.dylib";
#else
		const char *libname = "libheif.so";
#endif
		void *lib = dlopen(libname, RTLD_LAZY);
		if (!lib)
			break;
		if (!(hctxFree = dlsym(lib, "heif_context_free")))
			break;
		if (!(hctxMemory = dlsym(lib, "heif_context_read_from_memory_without_copy")))
			break;
		if (!(hctxHandle = dlsym(lib, "heif_context_get_primary_image_handle")))
			break;
		if (!(hndlWidth = dlsym(lib, "heif_image_handle_get_width")))
			break;
		if (!(hndlHeight = dlsym(lib, "heif_image_handle_get_height")))
			break;
		if (!(hndlDecode = dlsym(lib, "heif_decode_image")))
			break;
		if (!(himgChroma = dlsym(lib, "heif_image_get_chroma_format")))
			break;
		if (!(himgWidth = dlsym(lib, "heif_image_get_primary_width")))
			break;
		if (!(himgHeight = dlsym(lib, "heif_image_get_primary_height")))
			break;
		if (!(himgPlane = dlsym(lib, "heif_image_get_plane_readonly")))
			break;
		if (!(hctxAlloc = dlsym(lib, "heif_context_alloc")))
			break;
		struct herr (*init)(void *);
		if ((init = dlsym(lib, "heif_init")))
			init(NULL);
		rv |= 2;
	} while (0);
	return rv;
}

int
lazifEncode(struct lazifArgs *args)
{
	int rv = -1;
	struct frame *frame = imgCreate(args->width, args->height, 8, args->format);
	struct encoder *enc = encCreate();
	if (!frame || !enc) {
		snprintf(args->mesg, sizeof(args->mesg), "failed to create encoder");
		goto out;
	}

	for (int i = 0; i < 3; i++) {
		frame->yuvPlanes[i] = args->planes[i];
		frame->yuvRowBytes[i] = args->strides[i];
	}

	enc->maxThreads = 2;
	enc->speed = 10;

	struct rwdata out;
	memset(&out, 0, sizeof(out));
	int err = encWrite(enc, frame, &out);
	if (err) {
		snprintf(args->mesg, sizeof(args->mesg), "failed to encode");
		goto out;
	}
	args->data = out.data;
	args->datalen = out.size;
	rv = 0;
out:
	if (enc)
		encDestroy(enc);
	if (frame)
		imgDestroy(frame);
	return rv;
}

int
lazifDecodeAvif(struct lazifArgs *args)
{
	int rv = -1;
	struct decoder *dec = decCreate();
	if (!dec) {
		snprintf(args->mesg, sizeof(args->mesg), "failed to create decoder");
		goto out;
	}
	dec->maxThreads = 2;

	int err = setMemory(dec, args->data, args->datalen);
	if (err) {
		snprintf(args->mesg, sizeof(args->mesg), "failed to set memory");
		goto out;
	}
	err = decParse(dec);
	if (err) {
		snprintf(args->mesg, sizeof(args->mesg), "failed to decode");
		goto out;
	}
	if (nextImage(dec) == 0) {
		struct frame *frame = dec->image;
		if (frame->depth != 8) {
			snprintf(args->mesg, sizeof(args->mesg), "not 8 bit image: %d", frame->depth);
			goto out;
		}
		if (frame->yuvFormat != YUV444 && frame->yuvFormat != YUV420) {
			snprintf(args->mesg, sizeof(args->mesg), "not YUV420 image: %d", frame->yuvFormat);
			goto out;
		}
		args->format = frame->yuvFormat;
		args->width = frame->width;
		args->height = frame->height;
		for (int i = 0; i < 3; i++) {
			args->planes[i] = frame->yuvPlanes[i];
			args->strides[i] = frame->yuvRowBytes[i];
		}
		args->dec = dec;
		dec = NULL;
		rv = 0;
	}

out:
	if (dec)
		decDestroy(dec);
	return rv;
}

int
lazifConfigAvif(struct lazifArgs *args)
{
	int rv = -1;
	struct decoder *dec = decCreate();
	if (!dec) {
		snprintf(args->mesg, sizeof(args->mesg), "failed to create decoder");
		goto out;
	}
	dec->maxThreads = 2;

	int err = setMemory(dec, args->data, args->datalen);
	if (err) {
		snprintf(args->mesg, sizeof(args->mesg), "failed to set memory");
		goto out;
	}
	err = decParse(dec);
	if (err) {
		snprintf(args->mesg, sizeof(args->mesg), "failed to decode");
		goto out;
	}
	struct frame *frame = dec->image;

	args->width = frame->width;
	args->height = frame->height;

	rv = 0;
out:
	if (dec)
		decDestroy(dec);
	return rv;
}

int
lazifDecodeHeif(struct lazifArgs *args)
{
	int rv = -1;

	struct hctx *ctx = hctxAlloc();

	struct herr err = hctxMemory(ctx, args->data, args->datalen, NULL);
	if (err.code)
		goto out;
	struct hndl *handle;
	err = hctxHandle(ctx, &handle);
	if (err.code)
		goto out;
	struct himg *img;
	err = hndlDecode(handle, &img, 99, 99, NULL);
	if (err.code) {
		snprintf(args->mesg, sizeof(args->mesg), "failed to decode image");
		goto out;
	}

	int format = himgChroma(img);
	switch (format) {
	case 1: // chroma_420
		args->format = YUV420;
		break;
	case 3: // chroma_444
		args->format = YUV444;
		break;
	default:
		snprintf(args->mesg, sizeof(args->mesg), "not YUV420 image: %d", format);
		goto out;
	}

	args->width = himgWidth(img);
	args->height = himgHeight(img);
	for (int i = 0; i < 3; i++) {
		args->planes[i] = himgPlane(img, i, (int *)&args->strides[i]);
	}
	args->ctx = ctx;
	ctx = NULL;
	rv = 0;
out:
	if (ctx)
		hctxFree(ctx);
	return rv;
}

int
lazifConfigHeif(struct lazifArgs *args)
{
	int rv = -1;

	struct hctx *ctx = hctxAlloc();

	struct herr err = hctxMemory(ctx, args->data, args->datalen, NULL);
	if (err.code)
		goto out;
	struct hndl *handle;
	err = hctxHandle(ctx, &handle);
	if (err.code)
		goto out;

	args->width = hndlWidth(handle);
	args->height = hndlHeight(handle);

	rv = 0;
out:
	if (ctx)
		hctxFree(ctx);
	return rv;
}


int
lazifDecode(struct lazifArgs *args)
{
	if (args->datalen < 12)
		return -1;
	if (decCreate && memcmp(args->data + 4, "ftypavif", 8) == 0)
		return lazifDecodeAvif(args);
	if (hctxAlloc && memcmp(args->data + 4, "ftypheic", 8) == 0)
		return lazifDecodeHeif(args);
	return -1;
}

int
lazifConfig(struct lazifArgs *args)
{
	if (args->datalen < 12)
		return -1;
	if (decCreate && memcmp(args->data + 4, "ftypavif", 8) == 0)
		return lazifConfigAvif(args);
	if (hctxAlloc && memcmp(args->data + 4, "ftypheic", 8) == 0)
		return lazifConfigHeif(args);
	return -1;
}

void
lazifFree(struct lazifArgs *args)
{
	if (args->dec) {
		decDestroy(args->dec);
		args->dec = NULL;
	} else if (args->ctx) {
		hctxFree(args->ctx);
		args->ctx = NULL;
	} else {
		struct rwdata out;
		out.data = args->data;
		out.size = args->datalen;
		dataFree(&out);
	}
}
