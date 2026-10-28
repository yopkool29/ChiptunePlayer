/* DeliPlayer driver: parses the DELIRIUM tag array, runs the player
 * lifecycle (Check2 -> InitPlayer -> InitSound -> Interrupt ticks) and
 * streams Paula audio.  Public surface used by the Go binding. */
#include "emu.h"
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

/* Tag ids: DTP_TagBase = TAG_USER + "DT" = 0x80000000 + 0x4454 */
#define DTP_BASE 0x80004454u
enum {
	T_INTERNAL = 0, T_CUSTOM, T_REQDTVER, T_REQKICK, T_PLAYERVER,
	T_PLAYERNAME, T_CREATOR, T_CHECK1, T_CHECK2, T_EXTLOAD, T_INTERRUPT,
	T_STOP, T_CONFIG, T_USERCONFIG, T_SUBSONGRANGE,
	T_INITPLAYER, T_ENDPLAYER, T_INITSOUND, T_ENDSOUND, T_STARTINT,
	T_STOPINT, T_VOLUME, T_BALANCE, T_FASTER, T_SLOWER, T_NEXTPATT,
	T_PREVPATT, T_NEXTSONG, T_PREVSONG, T_SUBSONGTEST, T_NEWSUBSONGRANGE,
	T_DELIBASE, T_FLAGS, T_CHECKLEN, T_DESCR, T_DECRUNCH, T_CONVERT,
	T_NOTEPLAYER, T_NOTESTRUCT, T_NOTEINFO, T_NOTESIGNAL, T_PROCESS,
	T_PRIORITY, T_STACKSIZE, T_MSGPORT, T_APPEAR, T_DISAPPEAR,
	T_MODULENAME, T_FORMATNAME, T_AUTHORNAME, T_INITNOTE,
	T_MAXTAG
};

typedef struct {
	EmuCtx  *ctx;
	uint32_t code;                     /* code hunk base */
	uint32_t tags[T_MAXTAG + 1];       /* resolved tag addrs (0=absent) */
	uint32_t flags;
	int      inited;
} DeliPlayer;

extern void emu_set_tick(EmuCtx *c, uint32_t fn, int cia_mode);

/* the emulated tag array: {tag:u32, data:u32}* ended by tag==0 */
static int parse_tags(DeliPlayer *p, uint32_t code_base)
{
	/* header: moveq#-1;d0/rts or bra.w + 'DELIRIUM' + tag ptr */
	uint32_t tagp = rb32(code_base + 12); /* dc.l tagarray at +12 */
	char id[9]; emu_read(code_base + 4, id, 8); id[8] = 0;
	if (strcmp(id, "DELIRIUM")) return -1;
	for (int i = 0; i < 4096; i++) {
		uint32_t t = rb32(tagp + i * 8), v = rb32(tagp + i * 8 + 4);
		if (t == 0) break;
		if (t >= DTP_BASE && t - DTP_BASE <= T_MAXTAG)
			p->tags[t - DTP_BASE] = v;
		else if (t == 0x80000002) /* TAG_MORE? */ { tagp = v; i = -1; }
	}
	/* DTP_DeliBase: address where DT stores the globals pointer */
	if (p->tags[T_DELIBASE])
		wb32(p->tags[T_DELIBASE], p->ctx->deli_base);
	return 0;
}

DeliPlayer *deliplayer_open(const char *player_path)
{
	DeliPlayer *p = calloc(1, sizeof *p);
	p->ctx = emu_create();
	uint32_t cb = 0, cs = 0;
	if (!hunk_load(player_path, &cb, &cs) || cb == 0) {
		emu_destroy(p->ctx); free(p); return 0;
	}
	p->code = cb;
	emu_build_deliglobals(p->ctx);
	if (parse_tags(p, cb) != 0) { emu_destroy(p->ctx); free(p); return 0; }
	emu_set_tick(p->ctx, p->tags[T_INTERRUPT], 1);
	return p;
}

static int load_module(EmuCtx *c, const char *path)
{
	FILE *f = fopen(path, "rb");
	if (!f) return -1;
	fseek(f, 0, SEEK_END); long sz = ftell(f); fseek(f, 0, SEEK_SET);
	uint32_t a = emu_alloc(sz, 1);
	if (a == 0 || fread(emu_ram + a, 1, sz, f) != (size_t)sz) {
		fclose(f); return -1;
	}
	fclose(f);
	c->chkdata = a; c->chksize = sz;
	wb32(c->deli_base + 36, a);
	wb32(c->deli_base + 40, sz);
	c->list[0] = a; c->listsz[0] = sz; c->nlist = 1;
	/* path array: split dir/file so CopyDir/CopyFile resolve */
	char dir[512], base[256];
	const char *sl = strrchr(path, '/');
	snprintf(dir, sizeof dir, "%.*s/", sl ? (int)(sl - path) : 0, path);
	snprintf(base, sizeof base, "%s", sl ? sl + 1 : path);
	emu_wstr(rb32(c->deli_base + 24), dir);
	emu_wstr(rb32(c->deli_base + 28), base);
	uint32_t flagp = rb32(c->deli_base + 32);
	if (flagp < RAM_SIZE) emu_ram[flagp] = 0;
	return 0;
}

/* 0 = accepted */
int deliplayer_load(DeliPlayer *p, const char *mod_path)
{
	EmuCtx *c = p->ctx;
	if (load_module(c, mod_path) != 0) return -1;
	if (p->tags[T_CHECK2] && emu_call(c, p->tags[T_CHECK2], 0,0,0,0))
		return -2;
	if (p->tags[T_EXTLOAD] && emu_call(c, p->tags[T_EXTLOAD], 0,0,0,0))
		return -3;
	if (p->tags[T_INITPLAYER] && emu_call(c, p->tags[T_INITPLAYER], 0,0,0,0))
		return -4;
	/* InitSound has no return contract (per spec); run it for side effects */
	if (p->tags[T_INITSOUND]) emu_call(c, p->tags[T_INITSOUND], 0,0,0,0);
	if (p->tags[T_STARTINT]) emu_call(c, p->tags[T_STARTINT], 0,0,0,0);
	p->inited = 1;
	return 0;
}

/* render `frames` stereo frames: run the interrupt enough times for the
 * elapsed emulated time, then render Paula output for that span. */
void deliplayer_render(DeliPlayer *p, int16_t *out, int frames, int rate)
{
	EmuCtx *c = p->ctx;
	double per = emu_seconds_per_tick(c);
	if (c->tick_in <= 0) c->tick_in = per;
	/* interleave: render audio in <=tick slices, fire the interrupt at
	 * each tick boundary — like the real vblank/CIA-driven Amiga */
	int done = 0;
	while (done < frames) {
		int n = (int)(c->tick_in * rate);
		if (n < 1) n = 1;
		if (n > frames - done) n = frames - done;
		paula_render(out + done * 2, n, rate);
		done += n;
		c->tick_in -= (double)n / rate;
		if (c->tick_in <= 1.0 / rate) {
			emu_tick(c);
			c->tick_in += per;
		}
	}
	if (c->songend_flag) c->songend_flag = 0; /* caller polls via getter */
}

int  deliplayer_songend(DeliPlayer *p) { return p->ctx->songend_flag; }
void deliplayer_close(DeliPlayer *p)
{
	if (p->inited && p->tags[T_ENDSOUND]) emu_call(p->ctx, p->tags[T_ENDSOUND], 0,0,0,0);
	if (p->inited && p->tags[T_ENDPLAYER]) emu_call(p->ctx, p->tags[T_ENDPLAYER], 0,0,0,0);
	emu_destroy(p->ctx);
	free(p);
}

/* ---- meta helpers ---- */
uint32_t deliplayer_tag(DeliPlayer *p, int tag) { return p->tags[tag]; }
int deliplayer_subsongs(DeliPlayer *p, int *lo, int *hi)
{
	if (p->tags[T_NEWSUBSONGRANGE]) {
		int64_t r = emu_call(p->ctx, p->tags[T_NEWSUBSONGRANGE], 0,0,0,0);
		*lo = (int)(uint16_t)(r & 0xFFFF); *hi = (int)((uint32_t)r >> 16);
		return 1;
	}
	if (p->tags[T_SUBSONGRANGE]) {
		int64_t r = emu_call(p->ctx, p->tags[T_SUBSONGRANGE], 0,0,0,0);
		*lo = (int)(r & 0xFFFF); *hi = (int)((uint32_t)r >> 16);
		return 1;
	}
	*lo = *hi = (int)rb16(p->ctx->deli_base + 44);
	return 0;
}
void deliplayer_setsong(DeliPlayer *p, int n)
{
	EmuCtx *c = p->ctx;
	c->sndnum = (uint16_t)n;
	wb16(c->deli_base + 44, (uint16_t)n);
}
void deliplayer_setvol(DeliPlayer *p, int v)
{
	EmuCtx *c = p->ctx;
	c->snd_vol = v;
	wb16(c->deli_base + 46, (uint16_t)v);
	if (p->tags[T_VOLUME]) emu_call(c, p->tags[T_VOLUME], 0,0,0,0);
}
void deliplayer_setled(DeliPlayer *p, int on)
{
	p->ctx->led = on;
	wb16(p->ctx->deli_base + 52, (uint16_t)on);
	paula_set_led(on);
}
void deliplayer_mute(DeliPlayer *p, int ch, int on)
{
	(void)p;
	paula_mute(ch, on);
}
int deliplayer_voice_vol(DeliPlayer *p, int ch)
{
	(void)p;
	return paula_chan_level(ch);
}
void deliplayer_set_vu_lead_ms(int ms) { paula_set_vu_lead_ms(ms); }
