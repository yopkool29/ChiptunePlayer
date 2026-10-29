/* standalone test: ProRunner21 + a .mod -> render N seconds to s16 raw */
#include <stdio.h>
#include <stdint.h>
#include <string.h>

typedef struct DeliPlayer DeliPlayer;
DeliPlayer *deliplayer_open(const char *);
int  deliplayer_load(DeliPlayer *, const char *);
void deliplayer_render(DeliPlayer *, int16_t *, int, int);
int  deliplayer_songend(DeliPlayer *);
void deliplayer_close(DeliPlayer *);
uint32_t deliplayer_tag(DeliPlayer *, int);
int  deliplayer_subsongs(DeliPlayer *, int *, int *);

int main(int argc, char **argv)
{
	if (argc < 4) { fprintf(stderr, "usage: %s player module out.raw\n", argv[0]); return 1; }
	const char *player = argv[1];
	DeliPlayer *p = deliplayer_open(player);
	if (!p) { fprintf(stderr, "open failed\n"); return 1; }
	int r = deliplayer_load(p, argv[2]);
	printf("load=%d tags:", r);
	for (int i = 7; i < 52; i++) {
		uint32_t t = deliplayer_tag(p, i);
		if (t) printf(" %d:%x", i, t);
	}
	printf("\n");
	if (r) { deliplayer_close(p); return 1; }
	int lo, hi;
	deliplayer_subsongs(p, &lo, &hi);
	printf("subsongs %d..%d\n", lo, hi);
	FILE *o = fopen(argv[3], "wb");
	int16_t buf[44100 * 2];
	long peak = 0;
	for (int s = 0; s < 20 && !deliplayer_songend(p); s++) {
		memset(buf, 0, sizeof buf);
		deliplayer_render(p, buf, 44100, 44100);
		for (int i = 0; i < 44100 * 2; i++)
			if (buf[i] > peak) peak = buf[i]; else if (-buf[i] > peak) peak = -buf[i];
		if (o) fwrite(buf, 1, sizeof buf, o);
	}
	fclose(o);
	printf("peak=%ld\n", peak);
	deliplayer_close(p);
	return peak ? 0 : 2;
}
