/* Paula audio emulation: AUDx registers, DMA sample fetch, LED filter,
 * stereo mix at host sample rate.  PAL clock 3546895 Hz (7.09379/2). */
#include "emu.h"
#include <string.h>
#include <stdio.h>
#include <stdlib.h>
#include <math.h>

#define PAL_CLOCK 3546895.0

/* register offsets from 0xDFF000 */
#define R_VPOSR   0x004
#define R_VHPOSR  0x006
#define R_DMACONR 0x002
#define R_ADKCONR 0x010
#define R_INTENAR 0x01C
#define R_INTREQR 0x01E
#define R_DSKBYTR 0x01A
#define R_DMACON  0x096
#define R_INTENA  0x09A
#define R_INTREQ  0x09C
#define R_ADKCON  0x09E

#define AUD_OFF(ch) (0x0A0 + (ch) * 0x10)
/* per-channel: LCH LCL LEN PER VOL DAT */

typedef struct {
	uint32_t ptr;      /* next word to fetch (chip addr)            */
	uint32_t loop;     /* reload pointer (AUDxLCH/LCL)              */
	uint32_t len;      /* remaining words to fetch (0 written = 64k) */
	uint32_t llen;     /* reload length (AUDxLEN)                   */
	uint16_t per;      /* period in clocks                          */
	uint16_t vol;      /* volume 0..64                              */
	uint16_t dat;      /* AUDxDAT latched word (played first)       */
	int      use_dat;  /* dat word pending                          */
	uint16_t w;        /* word currently playing                    */
	int      have_w;   /* a word was fetched/played                 */
	int      half;     /* 0 = need new word, 1 = low byte pending   */
	double   frac;     /* clocks elapsed in current byte            */
	uint8_t  muted;
	uint8_t  end_irq;  /* audio-DMA-end irq already raised for len==0 */
} PChan;

static PChan ch[4];
static uint16_t dmacon, intena, intreq, adkcon;
static int ptrace = -1;
#define PTRACE(...) do { if (ptrace < 0) ptrace = getenv("PAULA_TRACE") != 0; \
	if (ptrace) fprintf(stderr, __VA_ARGS__); } while (0)
static int      led_filter;
long paula_fetch_calls, paula_fetch_nz;   /* debug counters */

static float   lvl_acc[4];  /* per-channel output peak for the VU  */
/* A500 output filter (WinUAE audio.cpp model): a static 2-pole RC
 * (6.2kHz + 20kHz) is ALWAYS in circuit; the LED switch adds a
 * 3-pole 7kHz lowpass on top.  LED is on after a real Amiga boot,
 * so filtered is the default replay state. */
static double  flt[4][5];   /* per-channel RC stages rc1..rc5      */

/* VU snapshot ring: the renderer runs ~one output buffer ahead of
 * the DAC, so a live level reads ~N ms in the future.  Snapshots are
 * stamped in song time (emu_time_ns); reads pick the entry at
 * emu_time - lead = what the ear hears *now*. */
#define LVL_RING 512            /* ~3 ms cadence -> ~1.5 s of history  */
static uint8_t  lvl_ring[LVL_RING][4];
static uint64_t lvl_ring_t[LVL_RING];
static int      lvl_ring_i;
static uint64_t lvl_next_snap;
static uint64_t vu_lead_ns = 120000000; /* pipeline depth guess (set by host) */
/* rendered-audio clock — NOT emu_time_ns, which also counts emulated
 * CPU cycles inside emu_call and would drift ~14% ahead on AHX */
static uint64_t audio_ns;

void paula_set_vu_lead_ms(int ms)
{
	if (ms < 0) ms = 0;
	vu_lead_ns = (uint64_t)ms * 1000000ULL;
}

void paula_init(void)
{
	memset(ch, 0, sizeof ch);
	memset(lvl_acc, 0, sizeof lvl_acc);
	memset(flt, 0, sizeof flt);
	memset(lvl_ring, 0, sizeof lvl_ring);
	memset(lvl_ring_t, 0, sizeof lvl_ring_t);
	lvl_ring_i = 0;
	lvl_next_snap = 0;
	audio_ns = 0;
	dmacon = 0x200;   /* host owns the master audio-DMA bit, like DeliTracker */
	intena = intreq = adkcon = 0;
	led_filter = 1;   /* power LED is on after a real Kickstart boot */
}

void paula_set_led(int on) { led_filter = on; }
void paula_mute(int c, int on) {
	if (c >= 0 && c < 4) {
		PTRACE("[paula] t=%.3f mute ch%d=%d\n", emu_time_ns / 1e9, c, on);
		ch[c].muted = on;
	}
}
int  paula_chan_vol(int c) { return (c >= 0 && c < 4) ? ch[c].vol : 0; }
/* VU level at the audible position, 0..64 scale.  The AUDxVOL
 * register alone lies — synth players (AHX) only write it on
 * instrument changes — so the meter follows rendered energy,
 * delayed by the output pipeline depth. */
int paula_chan_level(int c)
{
	if (c < 0 || c > 3) return 0;
	uint64_t t = audio_ns > vu_lead_ns ? audio_ns - vu_lead_ns : 0;
	/* circular ring: scan backwards from the newest entry */
	for (int k = 1; k <= LVL_RING; k++) {
		int i = lvl_ring_i - k;
		if (i < 0) i += LVL_RING;
		if (lvl_ring_t[i] && lvl_ring_t[i] <= t)
			return lvl_ring[i][c];
	}
	return 0;
}
int  paula_dma_pending(void) { return (dmacon & 0x200) != 0 && (dmacon & 0xF); }

/* Real Paula double-buffers AUDxLC/AUDxLEN: writes made while the
 * channel's DMA is on are the RELOAD (loop) segment, latched when the
 * current segment ends; writes while DMA is off take effect now.
 * ProTracker relies on this: it programs seg1 (full sample), starts DMA,
 * then writes seg2 (sustain loop, often len=2) for auto-repeat. */
static void chan_write(int c, uint32_t off, uint16_t v)
{
	PChan *p = &ch[c];
	int on = (dmacon & (1 << c)) != 0;
	switch (off & 0x0F) {
	case 0x00: /* LCH: high 16 bits of address (we use low 24) */
		p->loop = (p->loop & 0xFFFF) | ((uint32_t)v << 16);
		if (!on) p->ptr = p->loop;
		break;
	case 0x02: /* LCL: HW forces the pointer word-aligned (WinUAE: &0xFFFE) */
		p->loop = (p->loop & 0xFFFF0000) | (v & 0xFFFE);
		if (!on) p->ptr = p->loop;
		break;
	case 0x04: /* LEN: written value 0 means 65536 words on real HW */
		p->llen = v ? v : 0x10000;
		if (!on) p->len = p->llen;
		break;
	case 0x06: p->per = v ? v : 0x10000; break; /* PER=0 counts as 65536 */
	case 0x08: /* VOL: 7-bit register, clamped to 64 on real HW */
		p->vol = v & 0x7F;
		if (p->vol > 64) p->vol = 64;
		break;
	case 0x0A: p->dat = v; p->use_dat = 1; break;
	}
}

/* Byte write to a custom reg: OCS Paula writes the byte DUPLICATED
 * into both halves of the word-aligned register (WinUAE custom_bput:
 * rval = (v<<8)|v, addr&~1).  `move.b d0,AUDxVOL+1` thus lands the
 * value in vol — and likewise a byte store to PER/LEN/LC writes the
 * whole register, not one lane. */
void paula_write8(uint32_t reg, uint16_t v)
{
	paula_write(reg & ~1u, (uint16_t)(((v & 0xFF) << 8) | (v & 0xFF)));
}

extern unsigned int m68k_get_reg(void*, int);
void paula_write(uint32_t reg, uint16_t v)
{
	if (getenv("PAULA_TRACE"))
		fprintf(stderr, "[paula] w %03x=%04x pc=%06x a0=%06x a6=%06x\n",
		        reg, v, m68k_get_reg(0, 16 /*M68K_REG_PC*/),
		        m68k_get_reg(0, 8 /*A0*/), m68k_get_reg(0, 14 /*A6*/));
	if (reg >= AUD_OFF(0) && reg < AUD_OFF(0) + 0x40) {
		int c = (reg - AUD_OFF(0)) / 0x10;
		chan_write(c, reg & 0x0F, v);
		return;
	}
	switch (reg) {
	case R_DMACON: { /* bit15=set/clr, bit9=master, 0-3=channels */
		uint16_t old = dmacon;
		if (v & 0x8000) dmacon |= v & 0x7FFF;
		else            dmacon &= ~v & 0x7FFF;
		/* rising edge on a channel bit: DMA-on latches AUDxLC/LEN and
		 * restarts the fetch — a channel re-enabled mid-sample must
		 * resume from the reload pointer, not its stale position */
		uint16_t rising = dmacon & ~old & 0xF;
		PTRACE("[paula] t=%.3f DMACON %04x->%04x rise=%x\n",
		       emu_time_ns / 1e9, old, dmacon, rising);
		for (int c = 0; c < 4; c++)
			if (rising & (1 << c)) {
				ch[c].half = 0; ch[c].frac = 0; ch[c].have_w = 0;
				ch[c].ptr = ch[c].loop; ch[c].len = ch[c].llen;
				PTRACE("[paula]   ch%d start ptr=%06x len=%x vol=%d per=%d\n",
				       c, ch[c].ptr, ch[c].len, ch[c].vol, ch[c].per);
			}
		break;
	}
	case R_INTENA:
		if (v & 0x8000) intena |= v & 0x7FFF;
		else            intena &= ~v & 0x7FFF;
		break;
	case R_ADKCON:
		if (v & 0x8000) adkcon |= v & 0x7FFF;
		else            adkcon &= ~v & 0x7FFF;
		break;
	case R_INTREQ: /* bit15=set/clr — handlers ack their channel bit */
		if (v & 0x8000) intreq |= v & 0x7FFF;
		else            intreq &= ~v & 0x7FFF;
		break;
	}
}

uint16_t paula_read(uint32_t reg)
{
	if (getenv("PAULA_RTRACE"))
		fprintf(stderr, "[paula] r %03x\n", reg);
	switch (reg) {
	/* PAL raster readback: 313 lines, 64us each.  Players wait a frame
	 * by polling these (read $DFF004.l & 0x1ff00).  The beam position
	 * is derived from emu_time_ns so it also advances while the CPU
	 * runs inside emu_call. */
	case R_VPOSR:
		return (uint16_t)(((emu_time_ns / 64000) % 313) >> 8) & 1;
	case R_VHPOSR: {
		uint64_t line = (emu_time_ns / 64000) % 313;
		uint64_t hpos = (emu_time_ns % 64000) * 227 / 64000;
		return (uint16_t)(((line & 0xFF) << 8) | (hpos & 0xFF));
	}
	case R_DMACONR: return dmacon | 0x200;
	case R_INTENAR: return intena | 0x4000;
	case R_ADKCONR: return adkcon;
	case R_INTREQR: return intreq;
	}
	if (reg >= AUD_OFF(0) && reg < AUD_OFF(0) + 0x40) {
		int c = (reg - AUD_OFF(0)) / 0x10;
		PChan *p = &ch[c];
		switch (reg & 0x0F) {
		case 0x00: return (uint16_t)(p->loop >> 16);
		case 0x02: return (uint16_t)(p->loop & 0xFFFF);
		case 0x04: return p->len;
		case 0x06: return p->per;
		case 0x08: return p->vol;
		}
	}
	return 0;
}

/* --- audio rendering ----------------------------------------------------
 * Model: while a channel's DMA bit is on it outputs one byte every `per`
 * PAL clocks — high byte of the fetched word first, then the low byte.
 * ptr advances and len decrements at FETCH time (real HW).  When len
 * hits zero the channel would raise its audio interrupt and the player
 * would rewrite ptr/len; our shortcut reloads from loop/llen so sustain
 * loops keep sounding until the next tick rewrite.
 * A latched AUDxDAT word is emitted first when the channel starts.
 * --------------------------------------------------------------------- */
static void aud_irq(PChan *p)
{
	int c = (int)(p - ch);
	intreq |= 0x80 << c;
	if ((intena & 0x4000) && (intena & (0x80 << c)))
		emu_aud_irq(c);
}

/* Manual (non-DMA) AUDxDAT playback, channel off: a DAT write plays the
 * word once — hi byte then lo byte at `per` — and raises the channel
 * audio interrupt, then stops until another write.  audio.device's
 * non-DMA mode and DAT-poke synths are paced by that IRQ.  A write
 * while the interrupt is still pending only latches the word (HW:
 * `dat_written` waits for `!isirq` before re-entering play state). */
static int8_t manual_byte(PChan *p)
{
	if (p->half) {                    /* low byte of the word */
		p->half = 0; p->have_w = 0; /* one-shot: word done -> silence */
		return (int8_t)(p->w & 0xFF);
	}
	if (p->use_dat && !(intreq & (0x80 << (int)(p - ch)))) {
		p->w = p->dat; p->use_dat = 0;
		aud_irq(p);                 /* request + run handler if enabled */
		p->have_w = 1; p->half = 1;
		return (int8_t)(p->w >> 8); /* high byte first */
	}
	p->have_w = 0;
	return 0;
}

static int8_t next_byte(PChan *p)
{
	if (p->half) { p->half = 0; return (int8_t)(p->w & 0xFF); }
	if (p->use_dat) {
		p->w = p->dat; p->use_dat = 0;
	} else {
		if (p->len == 0) {
			/* DMA end: real HW raises the channel audio interrupt —
			 * streaming players (ArtOfNoise-8V …) feed the next
			 * chunk in the int4 handler (vector 0x70).  Level-
			 * triggered would re-fire forever on a dead channel,
			 * so edge-trigger: once per drained segment. */
			if (!p->end_irq) {
				p->end_irq = 1;
				aud_irq(p); /* request bit sets even with intena off */
			}
			if (p->len == 0) {
				if (!p->llen) { p->have_w = 0; return 0; }
				PTRACE("[paula] t=%.3f ch%d drain ptr=%06x -> loop %06x len %x (w=%04x)\n",
				       emu_time_ns / 1e9, (int)(p - ch), p->ptr,
				       p->loop, p->llen, p->w);
				p->ptr = p->loop; p->len = p->llen;
			}
			if (p->len) p->end_irq = 0;
		}
		if (p->ptr == 0) { p->have_w = 0; return 0; }
		p->w = rb16(p->ptr);
		paula_fetch_calls++; if (p->w) paula_fetch_nz++;
		p->ptr += 2;
		p->len--;
	}
	p->half = 1;
	p->have_w = 1;
	return (int8_t)(p->w >> 8);
}

/* WinUAE rc_calculate_a0: 1-pole RC coefficient with the bilinear
 * transform pre-warp, so the pole lands on the exact cutoff. */
static double rc_a0(int rate, int fc)
{
	if (fc >= rate / 2) return 1.0;
	double w = 2.0 * M_PI * fc / rate;
	w = tan(w / 2.0) * 2.0;
	return 1.0 / (1.0 + 1.0 / w);
}

void paula_render(int16_t *out, int frames, int rate)
{
	double clk_per_frame = PAL_CLOCK / rate;
	/* A500 filter poles (WinUAE audio.cpp FILTER_MODEL_A500) */
	const double a1 = rc_a0(rate, 6200), a2 = rc_a0(rate, 20000);
	const double af = rc_a0(rate, 7000);
	double lvl_rel = exp(-1.0 / ((double)rate * 0.12)); /* ~120ms VU release */
	uint64_t t0 = audio_ns;
	for (int i = 0; i < frames; i++) {
		double l = 0, r = 0;
		for (int c = 0; c < 4; c++) {
			PChan *p = &ch[c];
			lvl_acc[c] *= lvl_rel;       /* envelope decays even when off */
			int on = (dmacon & 0x200) && (dmacon & (1 << c));
			/* channel off: a leftover word drains once, and a latched
			 * AUDxDAT plays it manually (audio.device-style DAC) */
			int manual = !on && (p->use_dat || p->have_w);
			double v = 0;
			if ((on || manual) && p->per) {
				p->frac += clk_per_frame;
				int8_t s = 0;
				if (p->have_w) s = (int8_t)(p->w >> (p->half ? 8 : 0));
				while (p->frac >= p->per) {
					p->frac -= p->per;
					s = on ? next_byte(p) : manual_byte(p);
				}
				v = p->muted ? 0 : (double)s * p->vol;
			}
			/* the RC network keeps discharging while a channel is
			 * silent — freeze it instead and the residue pops back
			 * out at the next activation (WinUAE filters every
			 * channel every frame for the same reason) */
			double *f = flt[c];
			f[0] += a1 * (v - f[0]);       /* static RC: always on  */
			f[1] += a2 * (f[0] - f[1]);
			v = f[1];
			if (led_filter) {               /* power-LED lowpass     */
				f[2] += af * (v - f[2]);
				f[3] += af * (f[2] - f[3]);
				f[4] += af * (f[3] - f[4]);
				v = f[4];
			}
			double a = v < 0 ? -v : v;
			if (a > lvl_acc[c]) lvl_acc[c] = (float)a;
			if (c == 0 || c == 3) l += v; else r += v;
		}
		double L = l * 1.9, R = r * 1.9;   /* 2 ch/side, keep ~6% headroom */
		if (L > 32767) L = 32767; if (L < -32768) L = -32768;
		if (R > 32767) R = 32767; if (R < -32768) R = -32768;
		out[i * 2] = (int16_t)L;
		out[i * 2 + 1] = (int16_t)R;
		/* VU snapshot every ~3 ms of song time */
		uint64_t tf = t0 + (uint64_t)i * 1000000000ULL / (uint64_t)rate;
		if (tf >= lvl_next_snap) {
			for (int c = 0; c < 4; c++) {
				int s = (int)(lvl_acc[c] * 64.0f / 8128.0f);
				lvl_ring[lvl_ring_i][c] = (uint8_t)(s > 64 ? 64 : s);
			}
			lvl_ring_t[lvl_ring_i] = tf;
			lvl_ring_i = (lvl_ring_i + 1) % LVL_RING;
			lvl_next_snap = tf + 3000000;
		}
	}
	audio_ns += (uint64_t)frames * 1000000000ULL / (uint64_t)rate;
	emu_time_ns += (uint64_t)frames * 1000000000ULL / (uint64_t)rate;
}
