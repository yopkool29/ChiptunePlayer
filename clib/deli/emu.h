/* Mini DeliTracker host: emulated Amiga environment for running
 * original 68k DeliPlayer binaries (UADE-style). */
#ifndef DELI_EMU_H
#define DELI_EMU_H

#include <stdint.h>
#include <stddef.h>

/* ---- memory map (24-bit address space) ---- */
#define RAM_SIZE       (16u << 20)          /* 16 MB flat arena          */
#define REG_BASE       0x00DFF000u          /* custom chips window       */
#define REG_END        0x00E00000u
#define CIAA_BASE      0x00BFE000u          /* CIA-A (timer irq level 6) */
#define CIAA_END       0x00BF0000u
#define CIAB_BASE      0x00BFD000u          /* CIA-B (level 6 too, kbd)  */
#define CIAB_END       0x00BFE000u
#define KICK_BASE      0x00F80000u          /* fake kickstart ROM        */
#define HOST_MMIO      0x00FE0000u          /* host-call trampolines     */
#define SYSBASE_ADDR   0x0000F000u          /* fake exec.library base    */
#define HOSTSTACK      0x00100000u          /* initial A7 for host calls */

extern uint8_t *emu_ram;
/* global emulated-time clock (ns): advanced by CPU cycles in emu_call
 * and by rendered audio frames in paula_render — drives the VPOSR
 * raster readback players use for frame waits */
extern uint64_t emu_time_ns;

/* ---- Musashi memory callbacks ---- */
unsigned m68k_read_memory_8(unsigned addr);
unsigned m68k_read_memory_16(unsigned addr);
unsigned m68k_read_memory_32(unsigned addr);
unsigned m68k_read_pcrelative_8(unsigned addr);
unsigned m68k_read_pcrelative_16(unsigned addr);
unsigned m68k_read_pcrelative_32(unsigned addr);
unsigned m68k_read_imm_16(unsigned addr);
unsigned m68k_read_imm_32(unsigned addr);
unsigned m68k_read_disassembler_16(unsigned addr);
unsigned m68k_read_disassembler_32(unsigned addr);
void m68k_write_memory_8(unsigned addr, unsigned value);
void m68k_write_memory_16(unsigned addr, unsigned value);
void m68k_write_memory_32(unsigned addr, unsigned value);

/* ---- arena allocator (emulated-side) ---- */
void     mem_reset(void);
uint32_t emu_alloc(uint32_t size, uint32_t chip_only);
void     emu_free(uint32_t addr);
/* host-side convenience */
void     emu_write(uint32_t addr, const void *buf, uint32_t len);
void     emu_read(uint32_t addr, void *buf, uint32_t len);
void     emu_wstr(uint32_t addr, const char *s);
uint32_t emu_rstr(uint32_t addr, char *out, uint32_t max);

static inline uint16_t rb16(uint32_t a){ if (a>=RAM_SIZE-1)return 0; return (uint16_t)((emu_ram[a]<<8)|emu_ram[a+1]); }
static inline uint32_t rb32(uint32_t a){ if (a>=RAM_SIZE-3)return 0; return ((uint32_t)emu_ram[a]<<24)|((uint32_t)emu_ram[a+1]<<16)|((uint32_t)emu_ram[a+2]<<8)|emu_ram[a+3]; }
static inline void wb16(uint32_t a,uint16_t v){ if (a>=RAM_SIZE-1)return; emu_ram[a]=v>>8; emu_ram[a+1]=v; }
static inline void wb32(uint32_t a,uint32_t v){ if (a>=RAM_SIZE-3)return; emu_ram[a]=v>>24; emu_ram[a+1]=v>>16; emu_ram[a+2]=v>>8; emu_ram[a+3]=v; }

/* ---- paula ---- */
void  paula_init(void);
void  paula_write(uint32_t reg, uint16_t v);
uint16_t paula_read(uint32_t reg);
/* render n stereo frames into out (s16 interleaved), advance DMA */
void  paula_render(int16_t *out, int frames, int rate);
int   paula_chan_level(int c);   /* VU: peak output per channel, 0..64 */
void  paula_set_vu_lead_ms(int ms); /* output pipeline depth (VU sync) */
void  paula_mute(int c, int on);
void  paula_set_led(int on);
int   paula_dma_pending(void);   /* any channel DMA-on */

/* ---- cia ---- */
void   cia_init(void);
void   ciaa_write(uint32_t reg, uint8_t v);
uint8_t ciaa_read(uint32_t reg);
/* current timerA rate in ticks/sec (0 = stopped) */
double cia_timerA_hz(void);
/* level-6 int handler vector captured via SetIntVector-style write */
extern uint32_t cia_intvec;
extern uint32_t cia_intdata;    /* Interrupt struct passed in a1 */
/* feed CPU cycles; nonzero when CIA timer A underflows */
int     cia_pump(int cpu_cycles);
/* AddICRVector-style arm: latch + start timer A */
void    cia_arm_ta(uint16_t latch);

/* ---- hunk loader ---- */
/* load hunk file into arena, relocate, return code base or 0 */
uint32_t hunk_load(const char *path, uint32_t *code_base, uint32_t *code_size);

/* ---- host api ---- */
struct emu_ctx;
typedef struct emu_ctx EmuCtx;
struct emu_ctx {
	uint32_t pc_entry;          /* DeliPlayer code base */
	uint32_t deli_base;         /* DeliGlobals struct addr */
	uint32_t lvo_base;          /* exec.library LVO region */
	uint32_t sys_base;          /* exec.library base (at [4]) */
	int      pending_call;      /* trampoline slot fired */
	uint32_t call_args[4];
	int      snd_vol, snd_lbal, snd_rbal, led;
	uint16_t timer_val;         /* dtg_Timer */
	int      songend_flag;
	int      max_list, nlist;
	uint32_t list[64];          /* GetListData slots */
	uint32_t listsz[64];
	char     pathbuf[1024];
	uint32_t pathbuf_addr;
	uint32_t chkdata, chksize;
	uint16_t sndnum;
	double   tick_in;           /* seconds until next interrupt fires */
};

EmuCtx *emu_create(void);
void    emu_destroy(EmuCtx*);
void    emu_build_exec(EmuCtx*);              /* sysbase + LVO table */
void    emu_build_deliglobals(EmuCtx*);       /* DeliGlobals + trampolines */
/* run emulated routine at faddr with a5=deliglobals, args in d0/d1/a0/a1;
   returns d0 on emulated RTS */
int64_t  emu_call(EmuCtx*, uint32_t faddr, int64_t d0, int64_t d1, int64_t a0, int64_t a1);
/* one 50Hz (or dtg_Timer) tick step: call DTP_Interrupt or CIA vector */
void    emu_tick(EmuCtx*);
/* pump until the currently scheduled interrupt fires */
double  emu_seconds_per_tick(EmuCtx*);

#endif
