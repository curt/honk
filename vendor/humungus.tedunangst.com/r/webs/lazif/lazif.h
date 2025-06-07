struct lazifArgs {
	unsigned int width;
	unsigned int height;
	unsigned int format;
	unsigned char *planes[3];
	unsigned int strides[3];
	unsigned char *data;
	size_t datalen;
	void *dec;
	void *ctx;
	char mesg[128];
};

#define YUV444 1
#define YUV420 3

int lazifLoad(void);
int lazifEncode(struct lazifArgs *args);
int lazifDecode(struct lazifArgs *args);
int lazifConfig(struct lazifArgs *args);
void lazifFree(struct lazifArgs *args);

