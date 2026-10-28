/* Minimal CIA-A (8520) emulation: timer A latch/rate for the 50 Hz
 * (or CIA-timed) music tick.  Registers live at 0xBFE001 + reg*0x100. */
#include "emu.h"

#define CIAA_CLOCK 709379.0 /* PAL */

static uint8_t  creg[16];
static uint16_t talatch;
static double   ecyc;                 /* accumulated E-clock ticks */
uint32_t        cia_intvec;
uint32_t        cia_intdata;          /* Interrupt struct for a1   */

void cia_init(void)
{
	for (int i = 0; i < 16; i++) creg[i] = 0;
	talatch = 0xFFFF;
	ecyc = 0;
	cia_intvec = 0;
	cia_intdata = 0;
}

/* pump CPU cycles through CIA timer A (E-clock = CPU/10);
 * returns 1 each time the timer underflows = interrupt pending */
int cia_pump(int cpu_cycles)
{
	if (!(creg[0x0E] & 1)) { ecyc = 0; return 0; }
	ecyc += cpu_cycles / 10.0;
	if (talatch && ecyc >= talatch) { ecyc -= talatch; return 1; }
	return 0;
}

/* AddICRVector arms timer A like the real resource + score do:
 * latch = dtg_Timer, ICR enables the TA irq, CRA starts+loads */
void cia_arm_ta(uint16_t latch)
{
	talatch = latch ? latch : 0xFFFF;
	creg[0x0D] = 0x81;
	creg[0x0E] = 0x11;
	ecyc = 0;
}

void ciaa_write(uint32_t reg, uint8_t v)
{
	uint32_t r = (reg >> 8) & 0x0F;
	creg[r] = v;
	if (r == 0x04) talatch = (talatch & 0xFF00) | v;
	if (r == 0x05) talatch = (talatch & 0x00FF) | ((uint16_t)v << 8);
}

uint8_t ciaa_read(uint32_t reg)
{
	uint32_t r = (reg >> 8) & 0x0F;
	return creg[r];
}

double cia_timerA_hz(void)
{
	if (!(creg[0x0E] & 1)) return 0;      /* CRA START */
	if (talatch == 0) return 0;
	return CIAA_CLOCK / (double)talatch;
}
