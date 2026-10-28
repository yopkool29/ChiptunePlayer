/* DeliTracker host: fake exec.library + DeliGlobals, host-call
 * trampolines, and the 50Hz/CIA tick driver.
 *
 * Call-out mechanism: each host function lives behind a trampoline at
 * HOST_MMIO + slot*16 containing `move.b #slot,(scratch); rts`.  The
 * scratch write raises pending_call and ends the timeslice; the host
 * dispatches the API call, then resumes — the trampoline's `rts`
 * returns to player code naturally.
 */
#include "emu.h"
#include <stdio.h>
#include <string.h>
#include <strings.h>
#include <stdlib.h>
#include <ctype.h>
#include <dirent.h>
#include <errno.h>
#include <unistd.h>
#include <sys/stat.h>
#include "../musashi/m68k.h"

#define TRAMP_BASE   HOST_MMIO          /* +slot*16                    */
#define SCRATCH_ADDR (HOST_MMIO+0x0F00) /* move.b target             */
#define SCR_DOS      (HOST_MMIO+0x0F02) /* dos.library LVO idx       */
#define SCR_LIB      (HOST_MMIO+0x0F04) /* other fake lib LVO idx    */
#define SCR_UADE     (HOST_MMIO+0x0F06) /* uade.library LVO idx      */
#define SCR_CIA      (HOST_MMIO+0x0F08) /* cia?.resource LVO idx     */
#define CALL_DONE    0xFE               /* slot: called routine rts  */

/* extended slot ranges folded into pending_call by scratch address */
#define S_DOS_BASE   0x2000
#define S_LIB_BASE   0x3000
#define S_UADE_BASE  0x4000
#define S_CIA_BASE   0x5000

/* slots 0..31 exec LVOs, 32.. dtg functions */
#define S_LVO0        0x00
#define S_GETLISTDATA 0x20
#define S_LOADFILE    0x21
#define S_COPYDIR     0x22
#define S_COPYFILE    0x23
#define S_COPYSTRING  0x24
#define S_AUDIOALLOC  0x25
#define S_AUDIOFREE   0x26
#define S_STARTINT    0x27
#define S_STOPINT     0x28
#define S_SONGEND     0x29
#define S_CUTSUFFIX   0x2A
#define S_SETTIMER    0x2B
#define S_WAITDMA     0x2C
#define S_LOCKSCREEN  0x2D
#define S_UNLOCKSCR   0x2E
#define S_NOTEPLAYER  0x2F
#define S_ALLOCLIST   0x30
#define S_FREELIST    0x31
#define S_UADEDEBUG   0x32
#define S_UADEOPTS    0x33
#define S_CONVERTMOD  0x34
#define S_LIBUNIMPL   0x35          /* any LVO on a fake library     */
#define S_UNIMPL_EXEC 0x60          /* 0x60+i: unimpl exec LVO #i    */

/* dtg_Timer default: CIA-A latch for a 50 Hz PAL tick
 * (E-clock 709379 / 50 = 14188).  The old 0x2AC9 gave ~64.8 Hz —
 * every CIA-driven player (AHX …) ran ~30% fast. */
#define DTG_TIMER_DEFAULT 0x376C

static EmuCtx *G;        /* single instance for trampoline dispatch */

/* musashi fixed-name callbacks (m68kconf.h) */
int  your_int_ack_handler_function(int level) { (void)level; return M68K_INT_ACK_AUTOVECTOR; }
void your_bkpt_ack_handler_function(unsigned int data) { (void)data; }
void your_reset_handler_function(void) {}

/* instruction hook: ring buffer of recent PCs — dumped when a call
 * expires its budget or jumps below 0x100 */
#define PC_RING 64
static unsigned pc_ring[PC_RING];
static int      pc_ring_i;
void your_instruction_hook_function(unsigned int pc)
{
	static unsigned prev;
	if (pc >= 0x200000) { prev = pc; return; }   /* skip runaway region */
	if (pc < 0x100 && prev >= 0x100 && getenv("DELI_TRACE")) {
		fprintf(stderr,
			"[hook] LOWJUMP %06x->%04x d0=%x d1=%x d2=%x d3=%x a0=%x a1=%x a4=%x a5=%x a6=%x sp=%x sr=%x\n",
			prev, pc,
			m68k_get_reg(0, M68K_REG_D0), m68k_get_reg(0, M68K_REG_D1),
			m68k_get_reg(0, M68K_REG_D2), m68k_get_reg(0, M68K_REG_D3),
			m68k_get_reg(0, M68K_REG_A0), m68k_get_reg(0, M68K_REG_A1),
			m68k_get_reg(0, M68K_REG_A4), m68k_get_reg(0, M68K_REG_A5),
			m68k_get_reg(0, M68K_REG_A6), m68k_get_reg(0, M68K_REG_A7),
			m68k_get_reg(0, M68K_REG_SR));
		fprintf(stderr, "[hook] bytes@%x: %02x%02x %02x%02x %02x%02x %02x%02x\n",
			prev,
			prev < RAM_SIZE ? emu_ram[prev] : 0,
			prev + 1 < RAM_SIZE ? emu_ram[prev + 1] : 0,
			prev + 2 < RAM_SIZE ? emu_ram[prev + 2] : 0,
			prev + 3 < RAM_SIZE ? emu_ram[prev + 3] : 0,
			prev + 4 < RAM_SIZE ? emu_ram[prev + 4] : 0,
			prev + 5 < RAM_SIZE ? emu_ram[prev + 5] : 0,
			prev + 6 < RAM_SIZE ? emu_ram[prev + 6] : 0,
			prev + 7 < RAM_SIZE ? emu_ram[prev + 7] : 0);
	}
	prev = pc;
	if (pc == pc_ring[(pc_ring_i + PC_RING - 1) & (PC_RING - 1)])
		return;                            /* collapse tight loops  */
	pc_ring[pc_ring_i] = pc;
	pc_ring_i = (pc_ring_i + 1) & (PC_RING - 1);
}
static void pc_ring_dump(void)
{
	if (!getenv("DELI_TRACE")) return;
	fprintf(stderr, "[hist] last PCs:\n");
	for (int i = 0; i < PC_RING; i++) {
		unsigned p = pc_ring[(pc_ring_i + i) & (PC_RING - 1)];
		if (p) fprintf(stderr, "  %06x\n", p);
	}
}

/* ---------- trampoline/memory helpers ---------- */
static void put_tramp(int slot)
{
	uint32_t a = TRAMP_BASE + slot * 16;
	/* move.b #slot, SCRATCH_ADDR ; rts   (opcode 0x13FC = #imm->abs.l) */
	uint8_t code[16] = {
		0x13, 0xFC, 0x00, (uint8_t)slot,
		(uint8_t)(SCRATCH_ADDR >> 24), (uint8_t)(SCRATCH_ADDR >> 16),
		(uint8_t)(SCRATCH_ADDR >> 8), (uint8_t)SCRATCH_ADDR,
		0x4E, 0x75
	};
	emu_write(a, code, sizeof code);
}
static uint32_t tramp(int slot) { return TRAMP_BASE + slot * 16; }

void host_mmio_write(unsigned a, unsigned v)
{
	int slot;
	switch (a & 0xFF) {
	case 0x02: slot = S_DOS_BASE  | (v & 0xFFFF); break; /* dos.library  */
	case 0x04: slot = S_LIB_BASE  | (v & 0xFFFF); break; /* other libs   */
	case 0x06: slot = S_UADE_BASE | (v & 0xFFFF); break; /* uade.library */
	case 0x08: slot = S_CIA_BASE  | (v & 0xFFFF); break; /* cia?.resource */
	default:   slot = v & 0xFF; break;                 /* byte slots   */
	}
	G->pending_call = slot;
	m68k_end_timeslice();
}

/* ---------- exec.library ----------------------------------------------
 * SysBase at SYSBASE_ADDR; absolute long [4] points to it.  LVO table:
 * 6-byte `jmp abs.l` stubs at SYSBASE_ADDR + negative offset.
 * --------------------------------------------------------------------- */
typedef struct { int lvo; int slot; } LvoEnt;
#define LVO_ALLOCVEC   (-684)
#define LVO_ALLOCMEM   (-198)
#define LVO_ALLOCABS   (-204)
#define LVO_FREEMEM    (-210)
#define LVO_OPENLIB    (-552)
#define LVO_CLOselib   (-414)
#define LVO_COPYMEM    (-624)
#define LVO_COPYMEMQ   (-630)
#define LVO_FORBID     (-132)
#define LVO_PERMIT     (-138)
#define LVO_DISABLE    (-120)
#define LVO_ENABLE     (-126)
#define LVO_FINDTASK   (-294)
#define LVO_WAIT       (-318)
#define LVO_SIGNAL     (-324)
#define LVO_ADDPORT    (-354)
#define LVO_REMPORT    (-360)
#define LVO_PUTMSG     (-366)
#define LVO_GETMSG     (-372)
#define LVO_WAITPORT   (-384)
#define LVO_OPENDEV    (-444)
#define LVO_CLOSEDEV   (-450)
#define LVO_DOIO       (-456)
#define LVO_SETINTVEC  (-168)
#define LVO_SUPERVISOR (-30)
#define LVO_USERSTATE  (-0x9C)
#define LVO_SUPERSTATE (-0x96)
#define LVO_RAWDOFMT   (-522)
#define LVO_FINDNAME   (-276)
#define LVO_OLDOPLIB   (-408)
#define LVO_OPENRES    (-498)
#define LVO_CAUSE      (-180)

static const LvoEnt lvos[] = {
	{ LVO_ALLOCVEC, 0 }, { LVO_ALLOCMEM, 1 }, { LVO_ALLOCABS, 2 },
	{ LVO_FREEMEM, 3 }, { LVO_OPENLIB, 4 }, { LVO_CLOselib, 5 },
	{ LVO_COPYMEM, 6 }, { LVO_COPYMEMQ, 7 }, { LVO_FORBID, 8 },
	{ LVO_PERMIT, 9 }, { LVO_DISABLE, 10 }, { LVO_ENABLE, 11 },
	{ LVO_FINDTASK, 12 }, { LVO_WAIT, 13 }, { LVO_SIGNAL, 14 },
	{ LVO_ADDPORT, 15 }, { LVO_REMPORT, 16 }, { LVO_PUTMSG, 17 },
	{ LVO_GETMSG, 18 }, { LVO_WAITPORT, 19 }, { LVO_OPENDEV, 20 },
	{ LVO_CLOSEDEV, 21 }, { LVO_DOIO, 22 }, { LVO_SETINTVEC, 23 },
	{ LVO_SUPERVISOR, 24 }, { LVO_USERSTATE, 25 }, { LVO_SUPERSTATE, 26 },
	{ LVO_RAWDOFMT, 27 }, { LVO_FINDNAME, 28 },
	{ LVO_OLDOPLIB, 4 },            /* OldOpenLibrary = OpenLibrary   */
	{ LVO_OPENRES, 29 },            /* OpenResource -> fake resource  */
	{ LVO_CAUSE, 30 },              /* Cause: dispatch soft interrupt  */
};
#define NLVOS (sizeof(lvos) / sizeof(lvos[0]))

void emu_build_exec(EmuCtx *c)
{
	/* absolute 4 -> SysBase */
	wb32(4, SYSBASE_ADDR);
	/* fill the whole exec LVO space (-6..-702) with generic
	 * unimplemented stubs — a player calling an LVO we don't model
	 * gets a clean d0=0 instead of executing zeroed memory */
	for (int i = 0; i < 116; i++) {
		uint32_t a = SYSBASE_ADDR - (uint32_t)(i + 1) * 6;
		emu_ram[a] = 0x4E; emu_ram[a + 1] = 0xF9;
		wb32(a + 2, tramp(S_UNIMPL_EXEC + i));
	}
	/* LVO stubs */
	for (size_t i = 0; i < NLVOS; i++) {
		uint32_t a = SYSBASE_ADDR + lvos[i].lvo;
		emu_ram[a] = 0x4E; emu_ram[a + 1] = 0xF9;   /* jmp abs.l */
		wb32(a + 2, tramp(S_LVO0 + lvos[i].slot));
	}
	/* library name table inside sysbase: give exec.library an id string */
	emu_wstr(SYSBASE_ADDR + 600, "exec.library");
	wb32(SYSBASE_ADDR + 34, SYSBASE_ADDR + 600); /* ln_Name? ok best-effort */
	c->sys_base = SYSBASE_ADDR;
}

/* ---------- DeliGlobals -------------------------------------------------
 * Struct offsets per DeliPlayer.i.  Function-pointer entries point at
 * trampolines; scalar fields live in the emulated struct.
 * --------------------------------------------------------------------- */
enum {
	G_ASLBASE = 0, G_DOSBASE, G_INTUBASE, G_GFXBASE, G_GTBASE, G_RESVLIB,
	G_DIRPTR, G_FILEPTR, G_PATHPTR, G_CHKDATA, G_CHKSIZE,
	G_SNDNUM, G_SNDVOL, G_SNDLBAL, G_SNDRBAL, G_LED, G_TIMER,
};
#define DG_STRIDE_LONGS 16
#define DG_FUNC_BASE(n) ((n) * 4)

void emu_build_deliglobals(EmuCtx *c)
{
	uint32_t g = emu_alloc(512, 0);
	c->deli_base = g;
	/* fake library bases: point at sysbase-adjacent stubs */
	wb32(g + 0,  0);                    /* AslBase   */
	wb32(g + 4,  c->sys_base + 0x400);  /* DOSBase   */
	wb32(g + 8,  c->sys_base + 0x500);  /* Intuition */
	wb32(g + 12, c->sys_base + 0x600);  /* GfxBase   */
	wb32(g + 16, 0);                    /* GadTools  */
	wb32(g + 20, 0);
	/* dir/file/path scratch strings */
	uint32_t ps = emu_alloc(1024, 0);
	c->pathbuf_addr = ps;
	emu_ram[ps] = 0;
	wb32(g + 24, ps);          /* DirArrayPtr  */
	wb32(g + 28, ps + 256);    /* FileArrayPtr */
	wb32(g + 32, ps + 512);    /* PathArrayPtr */
	wb32(g + 36, c->chkdata);
	wb32(g + 40, c->chksize);
	wb16(g + 44, c->sndnum);              /* SndNum  */
	wb16(g + 46, 64);                     /* SndVol  */
	wb16(g + 48, 64); wb16(g + 50, 64);   /* L/R bal */
	wb16(g + 52, (uint16_t)c->led);       /* LED     */
	wb16(g + 54, c->timer_val ? c->timer_val : DTG_TIMER_DEFAULT); /* ~50Hz CIA timer */
	/* function pointers */
	static const int funcs[] = {
		S_GETLISTDATA, S_LOADFILE, S_COPYDIR, S_COPYFILE, S_COPYSTRING,
		S_AUDIOALLOC, S_AUDIOFREE, S_STARTINT, S_STOPINT, S_SONGEND,
		S_CUTSUFFIX, S_SETTIMER, S_WAITDMA, S_LOCKSCREEN, S_UNLOCKSCR,
		S_NOTEPLAYER, S_ALLOCLIST, S_FREELIST, 0, 0, 0,
	};
	uint32_t fp = g + 56;
	for (size_t i = 0; i < sizeof(funcs)/sizeof(funcs[0]); i++)
		wb32(fp + i * 4, funcs[i] ? tramp(funcs[i]) : 0);
}

/* ---------- host API dispatch ------------------------------------------ */
static uint32_t g_lib_dos, g_lib_gfx, g_lib_intu, g_lib_asl, g_lib_ciaa, g_lib_ciab, g_lib_uade;
static int      startint_on;
static uint32_t fake_task, fake_port;

/* append s at the tail of the path buffer string starting at p,
 * hard-bounded by pathbuf end AND arena end (p may be a wild
 * emulated pointer — never trust it) */
static void path_cat(uint32_t p, const char *s, uint32_t max)
{
	uint32_t limit = G->pathbuf_addr + 1023;
	if (limit >= RAM_SIZE) limit = RAM_SIZE - 1;
	uint32_t e = p;
	while (e < limit && emu_ram[e]) e++;
	while (e < limit && max-- && *s) emu_ram[e++] = (uint8_t)*s++;
	if (e < RAM_SIZE) emu_ram[e] = 0;
}

static uint32_t path_append(const char *seg)
{
	uint32_t p = rb32(G->deli_base + 32);      /* PathArrayPtr */
	path_cat(p, seg, 1023);
	return p;
}

static uint32_t host_alloc(uint32_t size, uint32_t flags)
{
	uint32_t a = emu_alloc(size ? size : 4, flags & 1);
	return a;
}

/* Per-LVO stub pair: at the library LVO offset we put `jmp code`,
 * and `code` runs `move.w #idx,(scratch.l); rts`.  The scratch write
 * reaches host_mmio_write with idx in v — each LVO is identifiable
 * without consuming trampoline slots. */
static void put_lib_stub(uint32_t lvo_addr, uint32_t scratch, int idx, uint32_t code)
{
	emu_ram[lvo_addr] = 0x4E; emu_ram[lvo_addr + 1] = 0xF9;
	wb32(lvo_addr + 2, code + idx * 12);
	uint8_t *p = emu_ram + code + idx * 12;
	p[0] = 0x33; p[1] = 0xFC;               /* move.w #imm,(abs.l) */
	p[2] = (uint8_t)(idx >> 8); p[3] = (uint8_t)idx;
	p[4] = (uint8_t)(scratch >> 24); p[5] = (uint8_t)(scratch >> 16);
	p[6] = (uint8_t)(scratch >> 8);  p[7] = (uint8_t)scratch;
	p[8] = 0x4E; p[9] = 0x75;               /* rts */
}

/* build a fake library: nlvo per-index stubs writing `scratch` */
static uint32_t fake_lib_ex(int nlvo, uint32_t scratch)
{
	uint32_t a = emu_alloc(64 + nlvo * 6 + nlvo * 12 + 32, 0);
	uint32_t base = a + nlvo * 6 + 16;
	uint32_t code = base + 64;
	for (int i = 0; i < nlvo; i++)
		put_lib_stub(base - (uint32_t)(i + 1) * 6, scratch, i, code);
	return base;
}

/* ---------- dos.library -------------------------------------------------
 * Minimal filesystem: assigns `X:name` map to <dos_root>/<x>/name on
 * the host (root = $DELI_DOSROOT or ./amiga-dos).  Plain paths stay
 * cwd-relative.  Handles and locks are BPTRs (addr>>2 by convention;
 * we just use a small index<<2). */
#define DOS_NFH 32
static FILE    *dos_fhs[DOS_NFH];
static char    *dos_locks[DOS_NFH];
static uint32_t dos_curdir;
static int      dos_ioerr;

static const char *dos_root(void)
{
	const char *r = getenv("DELI_DOSROOT");
	return (r && *r) ? r : "amiga-dos";
}

static void dos_translate(const char *ap, char *out, int sz)
{
	const char *c = strchr(ap, ':');
	if (c) {
		char vol[64];
		int n = (int)(c - ap);
		if (n > 63) n = 63;
		for (int i = 0; i < n; i++) vol[i] = (char)tolower((unsigned char)ap[i]);
		vol[n] = 0;
		snprintf(out, sz, "%s/%s%s%s", dos_root(), vol,
		         c[1] ? "/" : "", c + 1);
	} else if (ap[0] == '/' && ap[1] == '/') {
		snprintf(out, sz, "../%s", ap + 2);   /* Amiga // = parent */
	} else {
		snprintf(out, sz, "%s", ap);
	}
}

/* Amiga dos.library is case-insensitive; the host fs is not. Resolve
 * each path component by scanning its parent directory. Returns 0 and
 * writes the resolved path to out, or -1 if a component is missing. */
static int ci_resolve(const char *in, char *out, int sz)
{
	char cur[1024] = "";
	const char *p = in;
	if (*p == '/') { strcpy(cur, "/"); p++; }
	while (*p) {
		const char *e = strchr(p, '/');
		int n = e ? (int)(e - p) : (int)strlen(p);
		if (n > 255) n = 255;
		char comp[256];
		memcpy(comp, p, n); comp[n] = 0;
		p = e ? e + 1 : p + n;
		if (!n) continue;

		char cand[1280];
		snprintf(cand, sizeof cand, "%s%s%s", cur,
		         (cur[0] && cur[strlen(cur)-1] != '/') ? "/" : "", comp);
		if (access(cand, F_OK) == 0) {
			snprintf(cur, sizeof cur, "%s", cand);
			continue;
		}
		DIR *dd = opendir(cur[0] ? cur : ".");
		if (!dd) return -1;
		struct dirent *de;
		const char *hit = NULL;
		while ((de = readdir(dd)))
			if (!strcasecmp(de->d_name, comp)) { hit = de->d_name; break; }
		if (!hit) { closedir(dd); return -1; }
		snprintf(cur, sizeof cur, "%s%s%s", cur,
		         (cur[0] && cur[strlen(cur)-1] != '/') ? "/" : "", hit);
		closedir(dd);
	}
	snprintf(out, sz, "%s", cur);
	return 0;
}

/* dos_path translates ap and, when the literal path does not exist,
 * retries component-wise case-insensitively (e.g. a player opening
 * "ENV:eagleplayer/ahx.waves" finds env/EaglePlayer/ahx.waves). */
static void dos_path(const char *ap, char *out, int sz)
{
	dos_translate(ap, out, sz);
	struct stat st;
	if (stat(out, &st) != 0) {
		char rp[1024];
		if (ci_resolve(out, rp, sizeof rp) == 0)
			snprintf(out, sz, "%s", rp);
	}
}

static int      bptr_idx(uint32_t b) { return (int)(b >> 2) - 1; }
static uint32_t idx_bptr(int i)      { return (uint32_t)(i + 1) << 2; }

/* dos_reset releases every handle/lock a previous emulated instance
 * left open — probe rejects and crashed players never call Close. */
static void dos_reset(void)
{
	for (int i = 0; i < DOS_NFH; i++) {
		if (dos_fhs[i]) { fclose(dos_fhs[i]); dos_fhs[i] = 0; }
		if (dos_locks[i]) { free(dos_locks[i]); dos_locks[i] = 0; }
	}
	dos_curdir = 0;
	dos_ioerr = 0;
}

/* FileInfoBlock layout (260 bytes) */
static void fib_fill(uint32_t fib, const char *apath, long size, int isdir)
{
	uint8_t zero[260];
	memset(zero, 0, sizeof zero);
	emu_write(fib, zero, sizeof zero);
	const char *b = strrchr(apath, '/');
	b = b ? b + 1 : apath;
	const char *c = strrchr(b, ':');
	if (c) b = c + 1;
	char nm[108];
	snprintf(nm, sizeof nm, "%s", b);
	emu_wstr(fib + 8, nm);
	wb32(fib + 4,   isdir ? 2 : (uint32_t)-3); /* DirEntryType   */
	wb32(fib + 120, isdir ? 2 : (uint32_t)-3); /* EntryType      */
	wb32(fib + 124, (uint32_t)size);
	wb32(fib + 128, (uint32_t)((size + 511) / 512));
	wb32(fib + 132, 16000); /* DateStamp ds_Days (~2021) */
}

static uint32_t dos_dispatch(int idx, uint32_t d1, uint32_t d2, uint32_t d3, uint32_t a0)
{
	int lvo = -(idx + 1) * 6;
	if (getenv("DELI_TRACE")) {
		uint32_t sp = m68k_get_reg(0, M68K_REG_A7);
		fprintf(stderr, "[dos ] lvo=%d d1=%x d2=%x d3=%x ret=%x a6=%x\n",
			lvo, d1, d2, d3, rb32(sp), m68k_get_reg(0, M68K_REG_A6));
	}
	switch (lvo) {
	case -30: { /* Open(name=d1, mode=d2) */
		char nm[512], hp[600];
		emu_rstr(d1, nm, sizeof nm);
		dos_path(nm, hp, sizeof hp);
		const char *m = (d2 == 1006) ? "wb" : (d2 == 1004 ? "r+b" : "rb");
		FILE *f = fopen(hp, m);
		if (!f && d2 == 1004) f = fopen(hp, "rb");
		if (getenv("DELI_TRACE"))
			fprintf(stderr, "[dos ] Open(\"%s\" -> \"%s\") = %p%s%d\n", nm, hp, (void *)f,
			        f ? "" : " errno=", f ? 0 : errno);
		if (!f) { dos_ioerr = 205; return 0; }
		for (int i = 0; i < DOS_NFH; i++)
			if (!dos_fhs[i]) { dos_fhs[i] = f; dos_ioerr = 0; return idx_bptr(i); }
		fclose(f); dos_ioerr = 103; return 0;
	}
	case -36: { /* Close(fh=d1) */
		int i = bptr_idx(d1);
		if (i >= 0 && i < DOS_NFH && dos_fhs[i]) { fclose(dos_fhs[i]); dos_fhs[i] = 0; }
		dos_ioerr = 0; return 0xFFFFFFFF;
	}
	case -42: { /* Read(fh=d1, buf=d2, len=d3) */
		int i = bptr_idx(d1);
		if (i < 0 || i >= DOS_NFH || !dos_fhs[i] || d2 >= RAM_SIZE) { dos_ioerr = 206; return (uint32_t)-1; }
		if (d3 > RAM_SIZE - d2) d3 = RAM_SIZE - d2;
		size_t n = fread(emu_ram + d2, 1, d3, dos_fhs[i]);
		dos_ioerr = 0; return (uint32_t)n;
	}
	case -48: { /* Write(fh=d1, buf=d2, len=d3) */
		int i = bptr_idx(d1);
		if (i >= 0 && i < DOS_NFH && dos_fhs[i] && d2 < RAM_SIZE) {
			if (d3 > RAM_SIZE - d2) d3 = RAM_SIZE - d2;
			fwrite(emu_ram + d2, 1, d3, dos_fhs[i]);
		}
		return d3;
	}
	case -54: case -60: return 0;        /* Input/Output: no console */
	case -66: { /* Seek(fh=d1, pos=d2, mode=d3) */
		int i = bptr_idx(d1);
		if (i < 0 || i >= DOS_NFH || !dos_fhs[i]) { dos_ioerr = 206; return (uint32_t)-1; }
		int w = (d3 == 0) ? SEEK_CUR : (d3 == 1 ? SEEK_END : SEEK_SET);
		long old = ftell(dos_fhs[i]);
		fseek(dos_fhs[i], (int32_t)d2, w);
		return (uint32_t)old;
	}
	case -72: { /* DeleteFile(name=d1) */
		char nm[512], hp[600];
		emu_rstr(d1, nm, sizeof nm);
		dos_translate(nm, hp, sizeof hp);
		int ok = remove(hp) == 0;
		dos_ioerr = ok ? 0 : 205;
		return ok ? 0xFFFFFFFF : 0;
	}
	case -84: { /* Lock(name=d1, mode=d2) */
		char nm[512], hp[600];
		emu_rstr(d1, nm, sizeof nm);
		dos_path(nm, hp, sizeof hp);
		struct stat st;
		if (getenv("DELI_TRACE"))
			fprintf(stderr, "[dos ] Lock(\"%s\" -> \"%s\")\n", nm, hp);
		if (stat(hp, &st) != 0) { dos_ioerr = 205; return 0; }
		for (int i = 0; i < DOS_NFH; i++)
			if (!dos_locks[i]) { dos_locks[i] = strdup(nm); dos_ioerr = 0; return idx_bptr(i); }
		dos_ioerr = 103; return 0;
	}
	case -90: { /* UnLock(lock=d1) */
		int i = bptr_idx(d1);
		if (i >= 0 && i < DOS_NFH) { free(dos_locks[i]); dos_locks[i] = 0; }
		return 0xFFFFFFFF;
	}
	case -96: { /* DupLock(lock=d1) */
		int i = bptr_idx(d1);
		if (i < 0 || i >= DOS_NFH || !dos_locks[i]) return 0;
		for (int j = 0; j < DOS_NFH; j++)
			if (!dos_locks[j]) { dos_locks[j] = strdup(dos_locks[i]); return idx_bptr(j); }
		return 0;
	}
	case -102: { /* Examine(lock=d1, fib=d2) */
		int i = bptr_idx(d1);
		if (i < 0 || i >= DOS_NFH || !dos_locks[i] || d2 + 260 > RAM_SIZE) { dos_ioerr = 206; return 0; }
		char hp[600];
		dos_path(dos_locks[i], hp, sizeof hp);
		struct stat st;
		int ok = stat(hp, &st) == 0;
		fib_fill(d2, dos_locks[i], ok ? (long)st.st_size : 0, ok && S_ISDIR(st.st_mode));
		dos_ioerr = 0; return 0xFFFFFFFF;
	}
	case -108: dos_ioerr = 232; return 0;          /* ExNext: none    */
	case -114: return 0xFFFFFFFF;                  /* Info            */
	case -120: { /* CreateDir(name=d1) -> lock */
		char nm[512], hp[600];
		emu_rstr(d1, nm, sizeof nm);
		dos_translate(nm, hp, sizeof hp);
		mkdir(hp, 0755);
		for (int i = 0; i < DOS_NFH; i++)
			if (!dos_locks[i]) { dos_locks[i] = strdup(nm); return idx_bptr(i); }
		return 0;
	}
	case -126: { uint32_t old = dos_curdir; dos_curdir = d1; return old; } /* CurrentDir */
	case -132: return (uint32_t)dos_ioerr;          /* IoErr           */
	case -192: { /* DateStamp(a0) */
		if (a0 + 12 <= RAM_SIZE) { wb32(a0, 16000); wb32(a0 + 4, 0); wb32(a0 + 8, 0); }
		return 0;
	}
	case -198: return 0;                            /* Delay           */
	case -204: return 0;                            /* WaitForChar     */
	case -210: return 0;                            /* ParentDir       */
	case -216: return 0;                            /* IsInteractive   */
	default: return 0;
	}
}

/* dispatch one fired trampoline; regs are musashi-visible */
static void dispatch(int slot)
{
	uint32_t d0 = m68k_get_reg(0, M68K_REG_D0);
	uint32_t d1 = m68k_get_reg(0, M68K_REG_D1);
	uint32_t d2 = m68k_get_reg(0, M68K_REG_D2);
	uint32_t d3 = m68k_get_reg(0, M68K_REG_D3);
	uint32_t a0 = m68k_get_reg(0, M68K_REG_A0);
	uint32_t a1 = m68k_get_reg(0, M68K_REG_A1);
	uint32_t r = 0;
	if (getenv("DELI_TRACE"))
		fprintf(stderr, "[host] slot=%02x d0=%x d1=%x d2=%x d3=%x a0=%x a1=%x pc=%x\n",
			slot, d0, d1, d2, d3, a0, a1, m68k_get_reg(0, M68K_REG_PC));

	/* extended ranges: per-LVO library calls */
	if (slot >= S_DOS_BASE && slot < S_LIB_BASE) {
		r = dos_dispatch(slot & 0x1FFF, d1, d2, d3, a0);
		m68k_set_reg(M68K_REG_D0, r);
		return;
	}
	if (slot >= S_LIB_BASE && slot < S_UADE_BASE) {
		if (getenv("DELI_TRACE"))
			fprintf(stderr, "[lib ] lvo_idx=%d\n", slot & 0xFFF);
		m68k_set_reg(M68K_REG_D0, 0);
		return;
	}
	if (slot >= S_UADE_BASE && slot < S_CIA_BASE) {
		int idx = slot & 0xFFF;
		if (idx == 3) { /* -24 eagle options */
			slot = S_UADEOPTS;
			goto dtg;
		}
		if (getenv("DELI_TRACE"))
			fprintf(stderr, "[uade] lvo_idx=%d\n", idx);
		m68k_set_reg(M68K_REG_D0, 0);
		return;
	}
	if (slot >= S_CIA_BASE) {
		/* cia?.resource: -6 AddICRVector (a1=Interrupt, is_Code@+18,
		 * mask=d0) installs the player tick; -18 AbleICR grants the
		 * request; the rest are inert stubs. */
		int idx = slot & 0xFFF;
		uint32_t rr = 0;
		if (idx == 0) {          /* AddICRVector */
			cia_intvec = rb32(a1 + 18);
			cia_intdata = a1;
			/* d0 = ICR bit (0=TA, 1=TB); a6 = resource base selects
			 * the chip — players like ArtOfNoise-4V hook CIA-B
			 * timer B and program its latch themselves */
			cia_set_icr(m68k_get_reg(0, M68K_REG_A6) == g_lib_ciab,
			            d0 == 1);
			cia_arm(rb16(G->deli_base + 54)); /* latch = dtg_Timer */
			/* AddICRVector returns NULL on success — players test
			 * d0 and take an error path on nonzero, so returning
			 * the mask here made e.g. ArtOfNoise-4V skip its own
			 * latch programming and run at the stale default */
			rr = 0;
		} else if (idx == 2) {   /* AbleICR */
			rr = d0;
		}
		if (getenv("DELI_TRACE"))
			fprintf(stderr, "[cia ] lvo_idx=%d vec=%x\n", idx, cia_intvec);
		m68k_set_reg(M68K_REG_D0, rr);
		return;
	}

	if (slot < S_GETLISTDATA) {
		switch (slot) {
		case 0: case 1: case 2: /* AllocVec/AllocMem/AllocAbs */
			r = host_alloc(d0, d1); break;
		case 3: emu_free(a1); r = 0; break;
		case 4: { /* OpenLibrary */
			char nm[64]; emu_rstr(a1, nm, sizeof nm);
			if (strstr(nm, "uade"))      r = g_lib_uade;
			else if (strstr(nm, "dos"))  r = g_lib_dos;
			else if (strstr(nm, "graphics") || strstr(nm, "gfx")) r = g_lib_gfx;
			else if (strstr(nm, "intuition")) r = g_lib_intu;
			else if (strstr(nm, "asl"))  r = g_lib_asl;
			else                         r = g_lib_dos;
			break;
		}
		case 5: r = 0; break;                        /* CloseLibrary */
		case 6: case 7: {                            /* CopyMem(Quick) */
			if (a0 < RAM_SIZE && a1 < RAM_SIZE && d0 <= RAM_SIZE &&
			    d0 <= RAM_SIZE - a0 && d0 <= RAM_SIZE - a1)
				memmove(emu_ram + a1, emu_ram + a0, d0);
			r = a1; break;
		}
		case 8: case 9: case 10: case 11: r = 0; break; /* Forbid/Permit/Dis/Ena */
		case 12: r = fake_task; break;               /* FindTask */
		case 13: case 14: case 17: case 18: case 19: r = 0; break; /* waits */
		case 15: case 16: r = 0; break;              /* AddPort/RemPort */
		case 20: case 21: case 22: r = 0; break;     /* Dev calls */
		case 23: { /* SetIntVector: d0=intNumber, a1=struct Interrupt,
		              is_code@+18. int4 = audio channels (vec 0x70) */
			uint32_t code = rb32(a1 + 18);
			if (d0 == 4) { aud_vec = code; aud_vec_data = rb32(a1 + 14); }
			else         { cia_intvec = code; cia_intdata = a1; }
			r = a1; break;
		}
		case 24: case 25: case 26: r = 0; break;     /* supervisor */
		case 27: r = 0; break;                     /* RawDoFmt */
		case 28: r = 0; break;                     /* FindName */
		case 29: { /* OpenResource(a1=name): the base we hand out is
		            * how AddICRVector later knows which chip the
		            * vector lives on */
			char nm[32]; emu_rstr(a1, nm, sizeof nm);
			r = strstr(nm, "ciab") ? g_lib_ciab : g_lib_ciaa;
			break;
		}
		case 30: { /* Cause(a1=Interrupt): dispatch the server like
		              exec does — is_Code called with a1=is_Data,
		              a6=execbase; its rts returns to the Cause caller */
			uint32_t code = rb32(a1 + 18), data = rb32(a1 + 14);
			m68k_set_reg(M68K_REG_A1, data);
			m68k_set_reg(M68K_REG_A6, rb32(4));
			m68k_set_reg(M68K_REG_PC, code);
			return;                         /* don't touch d0       */
		}
		}
	} else switch (slot) {
	dtg:
	case S_GETLISTDATA: {
		uint32_t n = d0;
		if (n < (uint32_t)G->nlist) {
			m68k_set_reg(M68K_REG_A0, G->list[n]);
			r = G->listsz[n];
		} else { m68k_set_reg(M68K_REG_A0, 0); r = 0; }
		break;
	}
	case S_LOADFILE: {
		char path[512]; emu_rstr(rb32(G->deli_base + 32), path, sizeof path);
		FILE *f = fopen(path, "rb");
		if (!f) { r = 1; break; }
		fseek(f, 0, SEEK_END); long sz = ftell(f); fseek(f, 0, SEEK_SET);
		uint32_t a = emu_alloc(sz, 1);
		if (a && fread(emu_ram + a, 1, sz, f) == (size_t)sz) {
			if (G->nlist < 64) {
				G->list[G->nlist] = a; G->listsz[G->nlist] = sz; G->nlist++;
			}
			r = 0;
		} else r = 1;
		fclose(f);
		break;
	}
	case S_COPYDIR: {
		char dir[256];
		emu_rstr(rb32(G->deli_base + 24), dir, sizeof dir);
		path_cat(rb32(G->deli_base + 32), dir, 200);
		break;
	}
	case S_COPYFILE: {
		char fn[256]; emu_rstr(rb32(G->deli_base + 28), fn, sizeof fn);
		path_cat(rb32(G->deli_base + 32), fn, 200);
		break;
	}
	case S_COPYSTRING: {
		char s[256]; emu_rstr(a0, s, sizeof s);
		path_cat(rb32(G->deli_base + 32), s, 200);
		break;
	}
	case S_AUDIOALLOC: r = 0; break;
	case S_AUDIOFREE:  r = 0; break;
	case S_STARTINT:   startint_on = 1; r = 0; break;
	case S_STOPINT:    startint_on = 0; r = 0; break;
	case S_SONGEND:    G->songend_flag = 1; r = 0; break;
	case S_CUTSUFFIX: {
		uint32_t p = rb32(G->deli_base + 32);
		uint32_t e = p;
		while (e < G->pathbuf_addr + 1023 && e < RAM_SIZE && emu_ram[e]) e++;
		if (e - p >= 4 && emu_ram[e-4]=='.' &&
		    (emu_ram[e-3]=='x'||emu_ram[e-3]=='p')) emu_ram[e-4] = 0;
		break;
	}
	case S_SETTIMER:
		G->timer_val = rb16(G->deli_base + 54);
		/* reprogram the live timer too — on real HW SetTimer
		 * rewrites the CIA latch; keeping only timer_val left
		 * the armed rate stale (cia_tick_hz wins) */
		cia_arm(G->timer_val);
		r = 0; break;
	case S_WAITDMA:   r = 0; break;
	case S_LOCKSCREEN: r = 0; break;
	case S_UNLOCKSCR: r = 0; break;
	case S_NOTEPLAYER: r = 0; break;
	case S_ALLOCLIST: {
		uint32_t a = host_alloc(d0, d1);
		if (a && G->nlist < 64) {
			G->list[G->nlist] = a; G->listsz[G->nlist] = d0; G->nlist++;
		}
		r = a; break;
	}
	case S_FREELIST: emu_free(a1); r = 0; break;
	case S_UADEDEBUG: r = 0; break;
	case S_UADEOPTS: {
		/* eagle options list at a0: {op,name,dst1,dst2}* op0=end.
		 * op1=string: dst1=received-flag, dst2=buffer.
		 * op3=flag:   dst1=flag-byte, dst2=default. */
		uint32_t p = a0;
		for (int i = 0; i < 64; i++) {
			uint32_t op = rb32(p), nm = rb32(p + 4), d3 = rb32(p + 8), d4 = rb32(p + 12);
			p += 16;
			if (op == 0) break;
			if (op == 1) {
				char s[32]; emu_rstr(nm, s, sizeof s);
				if (!strcmp(s, "type")) {
					emu_wstr(d4, "typeptk"); /* -> .mod_ptk autodetect */
					if (d3 < RAM_SIZE) emu_ram[d3] = 1;
				}
			} else if (op == 3) {
				if (d3 < RAM_SIZE) emu_ram[d3] = 0;
			}
		}
		r = 1; /* options consumed */
		break;
	}
	case S_CONVERTMOD: r = 0xFFFFFFFF; break; /* no converter matched */
	}
	m68k_set_reg(M68K_REG_D0, r);
}

/* ---------- emu_call: run routine at faddr until RTS sentinel ---------- */
#define DONE_STACK_GUARD 0x1234

/* IRQ dispatcher stub (low RAM, above the vector table):
 * saves all regs, calls the CIA server with a1=is_Data/a6=execbase
 * (score.s convention), restores — Exec interrupt semantics. */
#define IRQ_STUB 0x00000800u
#define IRQ_HD   0x00000820u
#define IRQ_DT   0x00000824u
#define IRQ_RTS  0x00000828u

int64_t emu_call(EmuCtx *c, uint32_t faddr, int64_t d0, int64_t d1, int64_t a0, int64_t a1)
{
	if (faddr == 0) return 0;
	/* dead-stub tags: players compiled without a function point it at
	 * zeroed space (e.g. AHX's NewSubSongRange); executing it wanders
	 * off and scribbles RAM — treat as absent */
	if (faddr + 2 < RAM_SIZE && rb16(faddr) == 0)
		return 0;
	uint32_t sp = 0x00900000;            /* dedicated call stack */
	wb32(sp, tramp(CALL_DONE));          /* return addr = sentinel */
	/* point every stack register at the same top: whichever mode the
	 * CPU is in, rts lands on our sentinel */
	m68k_set_reg(M68K_REG_A7, sp);
	m68k_set_reg(M68K_REG_SP, sp);
	m68k_set_reg(M68K_REG_USP, sp);
	m68k_set_reg(M68K_REG_ISP, sp);
	m68k_set_reg(M68K_REG_A5, c->deli_base);
	m68k_set_reg(M68K_REG_D0, (uint32_t)d0);
	m68k_set_reg(M68K_REG_D1, (uint32_t)d1);
	m68k_set_reg(M68K_REG_A0, (uint32_t)a0);
	m68k_set_reg(M68K_REG_A1, (uint32_t)a1);
	m68k_set_reg(M68K_REG_PC, faddr);
	pc_ring_i = 0;

	long budget = 80000000;              /* ~11ms: raster waits need
	                                      * a full frame (~5ms)     */
	while (budget > 0) {
		uint32_t prev_pc = m68k_get_reg(0, M68K_REG_PC);
		int used = m68k_execute(20000);
		budget -= used;
		emu_time_ns += (uint64_t)used * 141; /* ~141 ns/cycle @7.09MHz */
		/* fire the captured CIA handler like a real IRQ: push the
		 * current PC and jump — the server's RTS resumes the flow.
		 * InitPlayer busy-waits on a flag the handler updates, so
		 * this must happen inside the call, not only at tick time. */
		if (cia_intvec && cia_pump(used)) {
			uint32_t pc0 = m68k_get_reg(0, M68K_REG_PC);
			uint32_t sp0 = m68k_get_reg(0, M68K_REG_A7) - 4;
			wb32(sp0, pc0);
			m68k_set_reg(M68K_REG_A7, sp0);
			/* dispatch via the register-saving stub: the handler
			 * may freely set a6=execbase etc., the interrupted
			 * code keeps all its registers (Exec semantics) */
			wb32(IRQ_HD, cia_intvec);
			wb32(IRQ_DT, rb32(cia_intdata + 14));
			m68k_set_reg(M68K_REG_PC, IRQ_STUB);
		}
		if (c->pending_call >= 0) {
			int slot = c->pending_call;
			c->pending_call = -1;
			if (slot == CALL_DONE) break;
			dispatch(slot);
		}
		uint32_t pc = m68k_get_reg(0, M68K_REG_PC);
		/* runaway guard: code lives in RAM or in the host-tramp
		 * window; a pc outside means the stack/flow is corrupt —
		 * bailing now keeps garbage execution from scribbling RAM */
		if (pc >= RAM_SIZE && pc < HOST_MMIO) break;
		if (pc < 0x100 && getenv("DELI_TRACE"))
			fprintf(stderr,
				"[host] JUMP->%04x from %06x d0=%x d1=%x d2=%x a0=%x a1=%x a4=%x a5=%x a6=%x\n",
				pc, prev_pc,
				m68k_get_reg(0, M68K_REG_D0), m68k_get_reg(0, M68K_REG_D1),
				m68k_get_reg(0, M68K_REG_D2), m68k_get_reg(0, M68K_REG_A0),
				m68k_get_reg(0, M68K_REG_A1), m68k_get_reg(0, M68K_REG_A4),
				m68k_get_reg(0, M68K_REG_A5), m68k_get_reg(0, M68K_REG_A6));
	}
	if (budget <= 0 && getenv("DELI_TRACE")) {
		fprintf(stderr, "[host] emu_call %x BUDGET EXPIRED pc=%x\n",
			faddr, m68k_get_reg(0, M68K_REG_PC));
		pc_ring_dump();
	}
	return m68k_get_reg(0, M68K_REG_D0);
}

/* ---------- tick pump ----------------------------------------------------
 * If the player provides DTP_Interrupt we call it per tick.  If it
 * instead installed a level-6 handler (CIA), we raise the IRQ and run
 * until RTE — the handler vector lives at 0x78 by default (auto) or at
 * cia_intvec when SetIntVector was captured.
 * ----------------------------------------------------------------------- */
static uint32_t g_tickfn;
static int      use_cia_vec;

void emu_set_tick(EmuCtx *c, uint32_t fn, int cia_mode)
{
	g_tickfn = fn;
	use_cia_vec = cia_mode;
}

double emu_seconds_per_tick(EmuCtx *c)
{
	double hz = cia_tick_hz();
	if (hz > 0) return 1.0 / hz;
	if (c->timer_val && c->timer_val != DTG_TIMER_DEFAULT)
		return c->timer_val / 709379.0;
	return 0.02; /* 50 Hz */
}

void emu_tick(EmuCtx *c)
{
	if (g_tickfn) { emu_call(c, g_tickfn, 0, 0, 0, 0); return; }
	if (use_cia_vec || cia_intvec) {
		/* enter the captured handler via the register-saving stub —
		 * never raise a CPU IRQ: a pending level-6 would fire
		 * mid-routine once the player lowers its IPL mask and
		 * vector 0x78 is unpopulated */
		uint32_t sp = 0x00800000;
		wb32(sp, tramp(CALL_DONE));
		m68k_set_reg(M68K_REG_A7, sp);
		wb32(IRQ_HD, cia_intvec ? cia_intvec : rb32(0x78));
		wb32(IRQ_DT, cia_intdata ? rb32(cia_intdata + 14) : 0);
		m68k_set_reg(M68K_REG_PC, IRQ_STUB);
		long budget = 5000000;
		while (budget > 0) {
			budget -= m68k_execute(20000);
			if (c->pending_call >= 0) {
				int slot = c->pending_call; c->pending_call = -1;
				if (slot == CALL_DONE) break;
				dispatch(slot);
			}
		}
		return;
	}
}

/* emu_aud_irq raises the channel's audio (int4, vector 0x70) handler.
 * Two conventions: a server captured via SetIntVector(4) is a plain
 * function (rts) — run it through the register-saving stub like the
 * CIA path.  A vector the player poked at 0x70 directly is a raw
 * hardware exception handler that ends with rte, so instead we hand
 * it a real exception frame: rte pops [SR][PC][format] and lands on
 * an rts that returns to the CALL_DONE trampoline. */
void emu_aud_irq(int ch)
{
	uint32_t h = aud_vec ? aud_vec : rb32(0x70);
	if (!h) return;
	uint32_t sp = 0x00800000;
	wb32(sp, tramp(CALL_DONE));
	if (aud_vec) {
		m68k_set_reg(M68K_REG_A7, sp);
		wb32(IRQ_HD, h);
		wb32(IRQ_DT, aud_vec_data);
		m68k_set_reg(M68K_REG_PC, IRQ_STUB);
	} else {
		sp -= 8;
		wb16(sp + 0, 0x2000);        /* SR: supervisor */
		wb32(sp + 2, IRQ_RTS);       /* PC: rte lands here */
		wb16(sp + 6, 0);             /* frame format 0 (68020 rte) */
		m68k_set_reg(M68K_REG_A7, sp);
		m68k_set_reg(M68K_REG_A1, 0);
		m68k_set_reg(M68K_REG_A6, rb32(4));
		m68k_set_reg(M68K_REG_PC, h);
	}
	long budget = 2000000;
	while (budget > 0) {
		budget -= m68k_execute(20000);
		if (G->pending_call >= 0) {
			int slot = G->pending_call;
			G->pending_call = -1;
			if (slot == CALL_DONE) break;
			dispatch(slot);
		}
	}
}

/* ---------- lifecycle --------------------------------------------------- */
EmuCtx *emu_create(void)
{
	EmuCtx *c = calloc(1, sizeof *c);
	emu_ram = calloc(RAM_SIZE, 1);
	mem_reset();
	c->pending_call = -1;
	c->timer_val = DTG_TIMER_DEFAULT;
	c->snd_vol = 64; c->snd_lbal = 64; c->snd_rbal = 64;
	G = c;
	paula_init();
	cia_init();
	dos_reset();
	emu_build_exec(c);
	g_lib_dos  = fake_lib_ex(240, SCR_DOS);   /* real per-LVO dispatch */
	g_lib_gfx  = fake_lib_ex(48, SCR_LIB);
	g_lib_intu = fake_lib_ex(48, SCR_LIB);
	g_lib_asl  = fake_lib_ex(48, SCR_LIB);
	/* cia?.resource must be a real LVO-table object: players jsr into
	 * it (AddICRVector installs the tick handler) */
	g_lib_ciaa = fake_lib_ex(8, SCR_CIA);
	g_lib_ciab = fake_lib_ex(8, SCR_CIA);
	fake_task = emu_alloc(64, 0); fake_port = emu_alloc(64, 0);
	for (int i = 0; i < 256; i++) put_tramp(i);
	/* fake uade.library: per-index stubs; -12 debug and -24 eagle
	 * options get real trampolines */
	g_lib_uade = fake_lib_ex(48, SCR_UADE);
	emu_ram[g_lib_uade - 12] = 0x4E; emu_ram[g_lib_uade - 11] = 0xF9;
	wb32(g_lib_uade - 10, tramp(S_UADEDEBUG));
	emu_ram[g_lib_uade - 24] = 0x4E; emu_ram[g_lib_uade - 23] = 0xF9;
	wb32(g_lib_uade - 22, tramp(S_UADEOPTS));
	/* Deli-Wizard ConvertMod stub at absolute 0xdc: returns -1
	 * (no clone converter matched) then rts */
	emu_ram[0xdc] = 0x70; emu_ram[0xdd] = 0xFF; /* moveq #-1,d0 */
	emu_ram[0xde] = 0x4E; emu_ram[0xdf] = 0x75; /* rts */
	/* IRQ dispatcher stub at 0x800:
	 *   movem.l d0-d7/a0-a6,-(sp)
	 *   movea.l (IRQ_HD).l,a0     ; handler
	 *   movea.l (IRQ_DT).l,a1     ; is_Data
	 *   movea.l (4).w,a6          ; execbase
	 *   jsr (a0)
	 *   movem.l (sp)+,d0-d7/a0-a6
	 *   rts */
	wb16(0x800, 0x48e7); wb16(0x802, 0xfffe);
	wb16(0x804, 0x2079); wb32(0x806, IRQ_HD);
	wb16(0x80a, 0x2279); wb32(0x80c, IRQ_DT);
	wb16(0x810, 0x2c78); wb16(0x812, 0x0004);
	wb16(0x814, 0x4e90);
	wb16(0x816, 0x4cdf); wb16(0x818, 0x7fff);
	wb16(0x81a, 0x4e75);
	wb16(IRQ_RTS, 0x4e75);           /* lone rts: raw rte handlers land here */
	m68k_init();
	/* 68020: players tagged *020 (AHX et al.) use 020+ ISA extensions
	 * (tst.l An, long bra, extb.l ...) — a strict 68000 would take
	 * illegal-instruction exceptions on them */
	m68k_set_cpu_type(M68K_CPU_TYPE_68020);
	m68k_set_instr_hook_callback(your_instruction_hook_function);
	m68k_pulse_reset();
	return c;
}

void emu_destroy(EmuCtx *c)
{
	free(emu_ram); emu_ram = 0;
	free(c);
}

EmuCtx *emu_ctx(void) { return G; }
