/* Minimal CIA (8520) emulation: only the music-tick timers are
 * modelled — enough for players that grab a cia?.resource interrupt
 * via AddICRVector and program the latch themselves.  CIA-A lives at
 * 0xBFE001.., CIA-B at 0xBFD000..; both have 16 regs at +0x100 steps:
 *   4/5 = timer A lo/hi   6/7 = timer B lo/hi
 *   0x0D = ICR            0x0E = CRA (timer A)   0x0F = CRB (timer B) */
#include "emu.h"
#include <stdio.h>
#include <stdlib.h>

#define CIAA_CLOCK 709379.0 /* PAL */

static uint8_t  creg[2][16];   /* [chip][reg] — chip 0=CIA-A, 1=CIA-B */
static uint16_t latch[2][2];   /* [chip][timer] — timer 0=A, 1=B */
static double   ecyc;          /* accumulated E-clock ticks */
static int      icr_chip, icr_tim; /* timer the captured vector rides on */
uint32_t        cia_intvec;
uint32_t        cia_intdata;          /* Interrupt struct for a1   */
uint32_t        aud_vec, aud_vec_data;/* captured int4 (audio) vec  */
uint32_t        aud_vecs[4], aud_vec_data4[4]; /* AUD0-3 via SetIntVector */

void cia_init(void)
{
	for (int c = 0; c < 2; c++)
		for (int i = 0; i < 16; i++) creg[c][i] = 0;
	creg[0][0] = 0x02; /* post-boot state: power LED on (audio LP in) */
	latch[0][0] = latch[0][1] = latch[1][0] = latch[1][1] = 0xFFFF;
	ecyc = 0;
	icr_chip = icr_tim = 0;
	cia_intvec = 0;
	cia_intdata = 0;
	aud_vec = 0;
	aud_vec_data = 0;
	for (int i = 0; i < 4; i++) { aud_vecs[i] = 0; aud_vec_data4[i] = 0; }
}

/* AddICRVector: d0 = ICR bit (0=timer A, 1=timer B), a6 = resource
 * base tells CIA-A vs CIA-B.  Called from delihost before arming. */
void cia_set_icr(int chip, int tim)
{
	icr_chip = chip;
	icr_tim = tim;
	ecyc = 0;
}

/* pump CPU cycles through the timer that owns the captured vector
 * (E-clock = CPU/10); returns 1 each time it underflows = irq pending */
int cia_pump(int cpu_cycles)
{
	uint16_t l;
	if (!(creg[icr_chip][0x0E + icr_tim] & 1)) { ecyc = 0; return 0; }
	ecyc += cpu_cycles / 10.0;
	l = latch[icr_chip][icr_tim];
	if (l && ecyc >= l) { ecyc -= l; return 1; }
	return 0;
}

/* AddICRVector arms the timer like the real resource + score do:
 * latch = dtg_Timer, ICR enables the irq, CR starts+loads.  Players
 * that want another rate rewrite the latch registers afterwards. */
void cia_arm(uint16_t l)
{
	if (getenv("DELI_TRACE"))
		fprintf(stderr, "[cia ] arm chip%d tim%d latch=%x -> %.1f Hz\n",
		        icr_chip, icr_tim, l,
		        CIAA_CLOCK / (l ? l : 0xFFFF));
	latch[icr_chip][icr_tim] = l ? l : 0xFFFF;
	creg[icr_chip][0x0D] |= (uint8_t)(0x80 | (1 << icr_tim));
	creg[icr_chip][0x0E + icr_tim] = 0x11;
	ecyc = 0;
}

static void cia_write(int chip, uint32_t reg, uint8_t v)
{
	uint32_t r = (reg >> 8) & 0x0F;
	creg[chip][r] = v;
	/* CIA-A PRA bit 1 drives the power LED which switches Paula's
	 * extra lowpass in/out of circuit — players toggle it for a
	 * brighter sound (bclr #1,$bfe001). */
	if (chip == 0 && r == 0x00)
		paula_set_led((v >> 1) & 1);
	switch (r) {
	case 0x04: latch[chip][0] = (uint16_t)((latch[chip][0] & 0xFF00) | v); break;
	case 0x05: latch[chip][0] = (uint16_t)((latch[chip][0] & 0x00FF) | ((uint16_t)v << 8)); break;
	case 0x06: latch[chip][1] = (uint16_t)((latch[chip][1] & 0xFF00) | v); break;
	case 0x07: latch[chip][1] = (uint16_t)((latch[chip][1] & 0x00FF) | ((uint16_t)v << 8)); break;
	}
}

void ciaa_write(uint32_t reg, uint8_t v) { cia_write(0, reg, v); }
void ciab_write(uint32_t reg, uint8_t v) { cia_write(1, reg, v); }

static uint8_t cia_read(int chip, uint32_t reg)
{
	uint32_t r = (reg >> 8) & 0x0F;
	return creg[chip][r];
}

uint8_t ciaa_read(uint32_t reg) { return cia_read(0, reg); }
uint8_t ciab_read(uint32_t reg) { return cia_read(1, reg); }

/* rate of the timer that owns the captured vector; 0 = not running */
double cia_tick_hz(void)
{
	uint16_t l;
	if (!(creg[icr_chip][0x0E + icr_tim] & 1)) return 0;
	l = latch[icr_chip][icr_tim];
	if (!l) return 0;
	return CIAA_CLOCK / (double)l;
}
