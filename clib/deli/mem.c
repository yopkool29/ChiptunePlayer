/* Emulated address space + bump allocator for the DeliTracker host. */
#include "emu.h"
#include <string.h>
#include <stdlib.h>
#include <stdio.h>

uint8_t *emu_ram;
uint64_t emu_time_ns;

/* arena: first 1MB is reserved (vectors, sysbase, exec stubs, scratch).
 * The bump pointer starts at 0x100000 (chipmem sample/module space must
 * be < 0x200000 for AUDxLCH; our entire arena is "chip" since paula reads
 * flat — we keep allocations in the first 8MB anyway). */
static uint32_t bump = 0x00100000;
static uint32_t allocs[1024];
static int      nallocs;

/* mem_reset must run on every new emulated instance: the bump arena
 * and the alloc table are globals, not per-EmuCtx — without this a few
 * loads exhaust the 16MB arena and every player starts failing. */
void mem_reset(void)
{
	bump = 0x00100000;
	nallocs = 0;
	emu_time_ns = 0;
}

uint32_t emu_alloc(uint32_t size, uint32_t chip_only)
{
	(void)chip_only; /* whole arena is chip-reachable in our model */
	uint32_t a = (bump + 3) & ~3u;
	/* size may come straight from emulated code (d0): compare in 64 bits
	 * so a huge size can't wrap a+size below the limit */
	if (size > RAM_SIZE - 0x10000 || (uint64_t)a + size > RAM_SIZE - 0x10000)
		return 0;
	bump = a + size;
	memset(emu_ram + a, 0, size);
	if (nallocs < 1024) allocs[nallocs++] = a;
	return a;
}

void emu_free(uint32_t addr)
{
	for (int i = 0; i < nallocs; i++)
		if (allocs[i] == addr) { allocs[i] = allocs[--nallocs]; return; }
}

void emu_write(uint32_t addr, const void *buf, uint32_t len)
{
	if (addr >= RAM_SIZE) return;
	if (len > RAM_SIZE - addr) len = RAM_SIZE - addr;
	memcpy(emu_ram + addr, buf, len);
}

void emu_read(uint32_t addr, void *buf, uint32_t len)
{
	if (addr >= RAM_SIZE) return;
	if (len > RAM_SIZE - addr) len = RAM_SIZE - addr;
	memcpy(buf, emu_ram + addr, len);
}

void emu_wstr(uint32_t addr, const char *s)
{ size_t n = strlen(s) + 1; emu_write(addr, s, (uint32_t)n); }

uint32_t emu_rstr(uint32_t addr, char *out, uint32_t max)
{
	uint32_t i = 0;
	while (i + 1 < max && addr + i < RAM_SIZE && emu_ram[addr + i]) {
		out[i] = emu_ram[addr + i]; i++;
	}
	out[i] = 0;
	return i;
}

/* ---- Musashi memory callbacks ---- */
static unsigned rd8(unsigned a){
	if (a >= REG_BASE && a < REG_END) return paula_read(a - REG_BASE) >> ((a & 1) ? 0 : 8);
	if (a >= CIAA_BASE && a < CIAA_END) return ciaa_read(a - CIAA_BASE);
	if (a >= CIAB_BASE && a < CIAB_END) return ciab_read(a - CIAB_BASE);
	if (a >= KICK_BASE && a < HOST_MMIO) return 0;
	if (a < RAM_SIZE) return emu_ram[a];
	return 0;
}
static unsigned rd16(unsigned a){
	if (a >= REG_BASE && a < REG_END) return paula_read(a - REG_BASE);
	if (a >= CIAA_BASE && a < CIAA_END) return ciaa_read(a - CIAA_BASE);
	if (a >= CIAB_BASE && a < CIAB_END) return ciab_read(a - CIAB_BASE);
	if (a >= KICK_BASE && a < HOST_MMIO) return 0;
	if (a + 1 < RAM_SIZE) return rb16(a);
	return 0;
}
static unsigned rd32(unsigned a){
	if (a >= REG_BASE && a < REG_END)
		return (paula_read(a - REG_BASE) << 16) | paula_read(a - REG_BASE + 2);
	if (a >= CIAA_BASE && a < CIAB_END) return 0;
	if (a >= KICK_BASE && a < HOST_MMIO) return 0;
	if (a + 3 < RAM_SIZE) return rb32(a);
	return (rd16(a) << 16) | rd16(a + 2);
}
static void watch_hit(unsigned a, unsigned v);
static void wr8(unsigned a, unsigned v){
	if (a >= REG_BASE && a < REG_END) { paula_write8(a - REG_BASE, (uint16_t)v); return; }
	if (a >= CIAA_BASE && a < CIAA_END) { ciaa_write(a - CIAA_BASE, (uint8_t)v); return; }
	if (a >= CIAB_BASE && a < CIAB_END) { ciab_write(a - CIAB_BASE, (uint8_t)v); return; }
	if (a >= HOST_MMIO) { extern void host_mmio_write(unsigned, unsigned); host_mmio_write(a, v); return; }
	if (a >= KICK_BASE) return;
	watch_hit(a, v);
	gdb_check_write(a, 1);
	if (a < RAM_SIZE) emu_ram[a] = v;
}
static uint32_t watch_a[8]; static int nwatch = -1;
static uint32_t watch_lo, watch_hi = (uint32_t)-1;   /* a-b range form */
static void watch_init(void)
{
	char *e = getenv("DELI_WATCH");
	if (!e) { nwatch = 0; return; }
	char buf[256]; strncpy(buf, e, sizeof buf - 1); buf[255] = 0;
	nwatch = 0;
	for (char *t = strtok(buf, ","); t && nwatch < 8; t = strtok(0, ",")) {
		char *dash = strchr(t, '-');
		if (dash) { watch_lo = strtoul(t, 0, 16);
		            watch_hi = strtoul(dash + 1, 0, 16); }
		else      watch_a[nwatch++] = strtoul(t, 0, 16);
	}
}
static void watch_hit(unsigned a, unsigned v)
{
	if (nwatch < 0) watch_init();
	extern unsigned int m68k_get_reg(void*, int);
	if (watch_hi != (uint32_t)-1 && a < watch_hi && a + 4 > watch_lo)
		fprintf(stderr, "[watch] %06x <- %06x pc=%06x\n", a, v,
		        m68k_get_reg(0, 16));
	for (int i = 0; i < nwatch; i++)
		if (a == watch_a[i] || a + 1 == watch_a[i] || a + 2 == watch_a[i] || a + 3 == watch_a[i])
			fprintf(stderr, "[watch] %06x <- %06x pc=%06x\n", a, v,
			        m68k_get_reg(0, 16));
}
static void wr16(unsigned a, unsigned v){
	if (a >= REG_BASE && a < REG_END) { paula_write(a - REG_BASE, (uint16_t)v); return; }
	if (a >= CIAA_BASE && a < CIAA_END) { ciaa_write(a - CIAA_BASE, (uint8_t)v); return; }
	if (a >= CIAB_BASE && a < CIAB_END) { ciab_write(a - CIAB_BASE, (uint8_t)v); return; }
	if (a >= HOST_MMIO) { extern void host_mmio_write(unsigned, unsigned); host_mmio_write(a, v); return; }
	if (a >= KICK_BASE) return;
	watch_hit(a, v);
	gdb_check_write(a, 2);
	wb16(a, (uint16_t)v);
}
static void wr32(unsigned a, unsigned v){
	if (a >= REG_BASE && a < REG_END) {         /* two 16-bit reg writes */
		paula_write(a - REG_BASE, (uint16_t)(v >> 16));
		paula_write(a - REG_BASE + 2, (uint16_t)v);
		return;
	}
	if (a >= CIAA_BASE && a < CIAB_END) return;
	if (a >= HOST_MMIO) { extern void host_mmio_write(unsigned, unsigned); host_mmio_write(a, v); return; }
	if (a >= KICK_BASE) return;
	watch_hit(a, v);
	gdb_check_write(a, 4);
	wb32(a, v);
}

unsigned m68k_read_memory_8(unsigned a){ return rd8(a); }
unsigned m68k_read_memory_16(unsigned a){ return rd16(a); }
unsigned m68k_read_memory_32(unsigned a){ return rd32(a); }
unsigned m68k_read_pcrelative_8(unsigned a){ return rd8(a); }
unsigned m68k_read_pcrelative_16(unsigned a){ return rd16(a); }
unsigned m68k_read_pcrelative_32(unsigned a){ return rd32(a); }
unsigned m68k_read_imm_16(unsigned a){ return rd16(a); }
unsigned m68k_read_imm_32(unsigned a){ return rd32(a); }
unsigned m68k_read_disassembler_16(unsigned a){ return rd16(a); }
unsigned m68k_read_disassembler_32(unsigned a){ return rd32(a); }
void m68k_write_memory_8(unsigned a, unsigned v){ wr8(a, v); }
void m68k_write_memory_16(unsigned a, unsigned v){ wr16(a, v); }
void m68k_write_memory_32(unsigned a, unsigned v){ wr32(a, v); }
