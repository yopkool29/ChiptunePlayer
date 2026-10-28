/* Amiga Hunk executable loader for the emulated arena.
 * Parses HUNK_HEADER / CODE / DATA / BSS / RELOC32, allocates each hunk
 * and applies 32-bit relocations against emulated load addresses. */
#include "emu.h"
#include <stdio.h>
#include <string.h>

static uint32_t be32(const uint8_t *p)
{ return ((uint32_t)p[0]<<24)|((uint32_t)p[1]<<16)|((uint32_t)p[2]<<8)|p[3]; }

/* returns base of first hunk; *code_base = address of the CODE hunk
 * (DeliPlayer entry points are offsets inside it). */
uint32_t hunk_load(const char *path, uint32_t *code_base, uint32_t *code_size)
{
	FILE *f = fopen(path, "rb");
	if (!f) return 0;
	fseek(f, 0, SEEK_END);
	long fsz = ftell(f);
	fseek(f, 0, SEEK_SET);
	uint8_t *d = malloc(fsz);
	if (fread(d, 1, fsz, f) != (size_t)fsz) { fclose(f); free(d); return 0; }
	fclose(f);

	if (fsz < 20 || be32(d) != 0x3F3) { free(d); return 0; }
	uint32_t off = 4;
	while (be32(d + off) != 0) {          /* resident names */
		uint32_t n = be32(d + off); off += 4 + n * 4;
	}
	off += 4;
	/* table_size, first_hunk, last_hunk */
	uint32_t table_size = be32(d+off), first = be32(d+off+4), last = be32(d+off+8);
	(void)table_size;
	off += 12;
	uint32_t nh = last - first + 1;
	if (nh == 0 || nh > 64) { free(d); return 0; }
	uint32_t sizes[64], flags[64];
	for (uint32_t i = 0; i < nh; i++) {
		uint32_t s = be32(d + off + i * 4);
		flags[i] = s >> 30;
		sizes[i] = (s & 0x3FFFFFFF) * 4;
	}
	off += nh * 4;

	uint32_t base[64];
	memset(base, 0, sizeof base);
	*code_base = 0; *code_size = 0;

	for (uint32_t i = 0; i < nh && off + 4 <= (uint32_t)fsz; ) {
		uint32_t tag = be32(d + off); off += 4;
		if (tag == 0x3F2) { i++; continue; }           /* HUNK_END        */
		if (tag == 0x3E9 || tag == 0x3E8) {            /* CODE / DATA     */
			uint32_t ln = be32(d + off) * 4; off += 4;
			uint32_t a = emu_alloc(sizes[i] ? sizes[i] : ln, 0);
			if (a == 0) { free(d); return 0; }
			base[i] = a;
			emu_write(a, d + off, ln);
			if (tag == 0x3E9 && *code_base == 0) { *code_base = a; *code_size = ln; }
			off += ln;
			continue;
		}
		if (tag == 0x3EA) {                            /* BSS             */
			base[i] = emu_alloc(sizes[i], 0);
			off += 4;
			continue;
		}
		if (tag == 0x3EC) {                            /* RELOC32         */
			uint32_t hh = 0;
			while (off + 4 <= (uint32_t)fsz) {
				uint32_t n = be32(d + off); off += 4;
				if (n == 0) break;
				uint32_t hnum = be32(d + off); off += 4;
				uint32_t b = (hnum >= first && hnum <= last) ? base[hnum - first] : 0;
				(void)hh;
				for (uint32_t k = 0; k < n && off + 4 <= (uint32_t)fsz; k++) {
					uint32_t ro = be32(d + off); off += 4;
					if (b && base[i])
						wb32(base[i] + ro, rb32(base[i] + ro) + b);
				}
			}
			continue;
		}
		if (tag == 0x3F0 || tag == 0x3F1 || tag == 0x3ED || tag == 0x3EE) {
			/* SYMBOL/DEBUG/RELOC16/RELOC8: skip block */
			uint32_t n = be32(d + off); off += 4 + n * 4;
			continue;
		}
		break; /* unknown tag: stop, partial load is still usable */
	}
	free(d);
	return base[0];
}
