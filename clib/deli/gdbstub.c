/* gdbstub.c — minimal GDB remote-protocol stub for the emulated m68k.
 *
 * Enabled with DELI_GDB=<port>: the emulator listens once, blocks until
 * gdb connects, then services stop events (breakpoints, write
 * watchpoints, single-step) from inside the m68k_execute loop.
 *
 *  gdb-multiarch -ex 'target remote :6666'
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>
#include <fcntl.h>
#include <arpa/inet.h>
#include <sys/socket.h>

#include "emu.h"
#include "../musashi/m68k.h"

extern void m68k_end_timeslice(void);

#define MAX_BP    32
#define MAX_WP    16
#define PKT_SIZE  8192

static int   gdb_port;                 /* 0 = disabled                */
static int   listen_fd = -1, conn = -1;
static int   no_ack;
static int   gdb_step;                 /* single-step mode            */
static int   gdb_stop;                 /* 0 none / 'B' bp / 'W' watch */
static unsigned gdb_watch_addr;
static int   detached;

static uint32_t bps[MAX_BP]; static int nbp;
static uint32_t wp_lo[MAX_WP], wp_hi[MAX_WP]; static int nwp;

/* ---------- packet io ------------------------------------------------- */
static int pkt_get(char *buf, int bufsz)
{
	char c;
	/* resync to '$' */
	for (;;) {
		if (recv(conn, &c, 1, 0) <= 0) return -1;
		if (c == '$') break;
		if (c == 0x03) return -2;          /* ctrl-c */
	}
	int n = 0;
	for (;;) {
		if (recv(conn, &c, 1, 0) <= 0) return -1;
		if (c == '#') break;
		if (n < bufsz - 1) buf[n++] = c;
	}
	buf[n] = 0;
	char cksum[2];
	recv(conn, cksum, 2, 0);               /* checksum (not verified)    */
	if (getenv("DELI_GDB_TRACE"))
		fprintf(stderr, "[gdb<-] %s\n", buf);
	if (!no_ack) send(conn, "+", 1, 0);
	return n;
}

static void pkt_put(const char *s)
{
	char out[PKT_SIZE + 8];
	unsigned sum = 0;
	for (const char *p = s; *p; p++) sum += (unsigned char)*p;
	if (getenv("DELI_GDB_TRACE"))
		fprintf(stderr, "[gdb->] %s\n", s);
	if (no_ack) snprintf(out, sizeof out, "$%s#%02x", s, sum & 0xFF);
	else        snprintf(out, sizeof out, "+$%s#%02x", s, sum & 0xFF);
	send(conn, out, strlen(out), 0);
}

/* ---------- registers ------------------------------------------------- */
/* m68k core layout: d0-d7, a0-a7, ps, pc (18 × 32-bit) */
static int reg_num(int mreg)
{
	switch (mreg) {
	case M68K_REG_D0: case M68K_REG_D1: case M68K_REG_D2: case M68K_REG_D3:
	case M68K_REG_D4: case M68K_REG_D5: case M68K_REG_D6: case M68K_REG_D7:
		return mreg - M68K_REG_D0;
	case M68K_REG_A0: case M68K_REG_A1: case M68K_REG_A2: case M68K_REG_A3:
	case M68K_REG_A4: case M68K_REG_A5: case M68K_REG_A6: case M68K_REG_A7:
		return 8 + (mreg - M68K_REG_A0);
	case M68K_REG_SR: return 16;
	case M68K_REG_PC: return 17;
	}
	return -1;
}

static void regs_get(char *out)
{
	char *p = out;
	for (int i = 0; i < 8; i++)
		p += sprintf(p, "%08x", (unsigned)m68k_get_reg(0, M68K_REG_D0 + i));
	for (int i = 0; i < 8; i++)
		p += sprintf(p, "%08x", (unsigned)m68k_get_reg(0, M68K_REG_A0 + i));
	p += sprintf(p, "%08x", (unsigned)m68k_get_reg(0, M68K_REG_SR));
	p += sprintf(p, "%08x", (unsigned)m68k_get_reg(0, M68K_REG_PC));
	/* gdb's builtin m68k register bank after pc: fp0-fp7 (96-bit) +
	 * fpcontrol/fpstatus/fpiaddr — 'x' marks them absent so the 'g'
	 * packet length matches the expected 180 bytes */
	memset(p, 'x', 8 * 24 + 3 * 8);
	p += 8 * 24 + 3 * 8;
	*p = 0;
}

/* ---------- command handling ------------------------------------------ */
/* no target.xml served: gdb keeps the arch set with
 * 'set architecture m68k' and its builtin register layout */
static const char *target_xml =
	"<?xml version=\"1.0\"?>"
	"<!DOCTYPE target SYSTEM \"gdb-target.dtd\">"
	"<target><architecture>m68k</architecture>"
	"<feature name=\"org.gnu.gdb.m68k.core\">"
	"<reg name=\"d0\" bitsize=\"32\"/><reg name=\"d1\" bitsize=\"32\"/>"
	"<reg name=\"d2\" bitsize=\"32\"/><reg name=\"d3\" bitsize=\"32\"/>"
	"<reg name=\"d4\" bitsize=\"32\"/><reg name=\"d5\" bitsize=\"32\"/>"
	"<reg name=\"d6\" bitsize=\"32\"/><reg name=\"d7\" bitsize=\"32\"/>"
	"<reg name=\"a0\" bitsize=\"32\"/><reg name=\"a1\" bitsize=\"32\"/>"
	"<reg name=\"a2\" bitsize=\"32\"/><reg name=\"a3\" bitsize=\"32\"/>"
	"<reg name=\"a4\" bitsize=\"32\"/><reg name=\"a5\" bitsize=\"32\"/>"
	"<reg name=\"a6\" bitsize=\"32\"/><reg name=\"a7\" bitsize=\"32\"/>"
	"<reg name=\"ps\" bitsize=\"32\"/>"
	"<reg name=\"pc\" bitsize=\"32\" type=\"code_ptr\"/>"
	"</feature></target>";

static void stop_reply(void)
{
	char buf[64];
	if (gdb_stop == 'W')
		snprintf(buf, sizeof buf, "T05watch:%x;", gdb_watch_addr);
	else
		snprintf(buf, sizeof buf, "S05");
	gdb_stop = 0;
	pkt_put(buf);
}

/* returns 1 when execution should resume (c/s), 0 to keep serving */
static int handle(char *pkt, int n)
{
	char out[PKT_SIZE];
	switch (pkt[0]) {
	case '?': stop_reply(); return 0;
	case 'H': pkt_put("OK"); return 0;
	case 'q':
		if (!strncmp(pkt, "qSupported", 10)) {
			pkt_put("PacketSize=2000;swbreak+;hwbreak+;QStartNoAckMode+");
		} else if (!strncmp(pkt, "qXfer:features:read:target.xml:", 31)) {
			unsigned off = strtoul(pkt + 31, 0, 16);
			int len = strlen(target_xml) - off;
			if (len <= 0) { pkt_put("l"); return 0; }
			out[0] = 'l';
			strncpy(out + 1, target_xml + off, PKT_SIZE - 2);
			out[1 + len] = 0;
			pkt_put(out);
		} else if (!strncmp(pkt, "qfThreadInfo", 12)) pkt_put("m1");
		else if (!strncmp(pkt, "qsThreadInfo", 12)) pkt_put("l");
		else if (!strncmp(pkt, "qC", 2)) pkt_put("QC1");
		else if (!strncmp(pkt, "qAttached", 9)) pkt_put("1");
		else pkt_put("");
		return 0;
	case 'Q':
		if (!strcmp(pkt, "QStartNoAckMode")) { no_ack = 1; pkt_put("OK"); }
		else pkt_put("");
		return 0;
	case 'v':
		if (!strncmp(pkt, "vCont?", 6)) { pkt_put("vCont;c;s"); return 0; }
		if (!strncmp(pkt, "vCont;c", 7)) return 1;
		if (!strncmp(pkt, "vCont;s", 7)) { gdb_step = 1; return 1; }
		pkt_put(""); return 0;
	case 'g': regs_get(out); pkt_put(out); return 0;
	case 'G': {
		const char *p = pkt + 1; char hex[9]; hex[8] = 0;
		static const int order[18] = {
			M68K_REG_D0, M68K_REG_D1, M68K_REG_D2, M68K_REG_D3,
			M68K_REG_D4, M68K_REG_D5, M68K_REG_D6, M68K_REG_D7,
			M68K_REG_A0, M68K_REG_A1, M68K_REG_A2, M68K_REG_A3,
			M68K_REG_A4, M68K_REG_A5, M68K_REG_A6, M68K_REG_A7,
			M68K_REG_SR, M68K_REG_PC };
		for (int i = 0; i < 18 && p[0] && p[7]; i++, p += 8) {
			memcpy(hex, p, 8);
			m68k_set_reg(order[i], strtoul(hex, 0, 16));
		}
		pkt_put("OK"); return 0;
	}
	case 'p': {
		int idx = strtoul(pkt + 1, 0, 16);
		static const int order[18] = {
			M68K_REG_D0, M68K_REG_D1, M68K_REG_D2, M68K_REG_D3,
			M68K_REG_D4, M68K_REG_D5, M68K_REG_D6, M68K_REG_D7,
			M68K_REG_A0, M68K_REG_A1, M68K_REG_A2, M68K_REG_A3,
			M68K_REG_A4, M68K_REG_A5, M68K_REG_A6, M68K_REG_A7,
			M68K_REG_SR, M68K_REG_PC };
		if (idx >= 0 && idx < 18) {
			snprintf(out, sizeof out, "%08x",
			         (unsigned)m68k_get_reg(0, order[idx]));
			pkt_put(out);
		} else pkt_put("E01");
		return 0;
	}
	case 'm': {
		unsigned a; int len;
		sscanf(pkt + 1, "%x,%d", &a, &len);
		if (a >= RAM_SIZE || a + len > RAM_SIZE) { pkt_put("E02"); return 0; }
		char *q = out;
		for (int i = 0; i < len; i++) q += sprintf(q, "%02x", emu_ram[a + i]);
		pkt_put(out); return 0;
	}
	case 'M': {
		unsigned a; int len;
		char *col;
		sscanf(pkt + 1, "%x,%d", &a, &len);
		col = strchr(pkt, ':');
		if (!col || a >= RAM_SIZE) { pkt_put("E03"); return 0; }
		col++;
		for (int i = 0; i < len && col[0] && col[1]; i++, col += 2) {
			unsigned b; sscanf(col, "%02x", &b);
			emu_ram[a + i] = (uint8_t)b;
		}
		pkt_put("OK"); return 0;
	}
	case 'Z': case 'z': {
		int set = pkt[0] == 'Z';
		int type = pkt[1] - '0';
		char *comma = strchr(pkt, ',');
		uint32_t a = comma ? strtoul(comma + 1, 0, 16) : 0;
		if (type == 0) {
			if (set && nbp < MAX_BP) bps[nbp++] = a;
			else if (!set)
				for (int i = 0; i < nbp; i++)
					if (bps[i] == a) { bps[i] = bps[--nbp]; break; }
		} else if (type == 2 || type == 4) {   /* write/access watch */
			int len = 4;
			char *k2 = comma ? strchr(comma + 1, ',') : 0;
			if (k2) len = atoi(k2 + 1);
			if (set && nwp < MAX_WP) {
				wp_lo[nwp] = a; wp_hi[nwp] = a + len; nwp++;
			} else if (!set)
				for (int i = 0; i < nwp; i++)
					if (wp_lo[i] == a) {
						wp_lo[i] = wp_lo[nwp - 1];
						wp_hi[i] = wp_hi[nwp - 1];
						nwp--; break;
					}
		}
		pkt_put("OK"); return 0;
	}
	case 'c': return 1;
	case 's': gdb_step = 1; return 1;
	case 'k': case 'D':
		pkt_put("OK");
		close(conn); conn = -1; detached = 1;
		return 1;
	case 'T': pkt_put("OK"); return 0;      /* thread-alive check */
	default:  pkt_put(""); return 0;
	}
}

/* blocking packet loop until continue/step/detach.  On a stop event the
 * target reports the stop reply first; at initial connect gdb drives
 * first (it sends ? once ready) so no unsolicited packet may be sent. */
static void gdb_serve(int announce)
{
	if (announce) stop_reply();
	char pkt[PKT_SIZE];
	for (;;) {
		int n = pkt_get(pkt, sizeof pkt);
		if (n < 0) { close(conn); conn = -1; detached = 1; return; }
		if (handle(pkt, n)) return;
	}
}

/* ---------- integration hooks ---------------------------------------- */
void gdb_emu_start(void)
{
	const char *e = getenv("DELI_GDB");
	if (!e || gdb_port) return;
	gdb_port = atoi(e);
	if (!gdb_port) return;
	listen_fd = socket(AF_INET, SOCK_STREAM, 0);
	int one = 1;
	setsockopt(listen_fd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof one);
	struct sockaddr_in sa = {
		.sin_family = AF_INET,
		.sin_port   = htons(gdb_port),
		.sin_addr.s_addr = htonl(INADDR_LOOPBACK) };
	if (bind(listen_fd, (void *)&sa, sizeof sa) || listen(listen_fd, 1)) {
		perror("[gdb] listen");
		close(listen_fd); listen_fd = -1; gdb_port = 0; return;
	}
	fprintf(stderr, "[gdb] waiting on :%d — gdb-multiarch -ex 'target remote :%d'\n",
		gdb_port, gdb_port);
	conn = accept(listen_fd, 0, 0);
	if (conn < 0) { perror("[gdb] accept"); gdb_port = 0; return; }
	fprintf(stderr, "[gdb] connected\n");
	/* enter the packet loop silently: gdb sends ? when it wants the
	 * current stop state — answering early desyncs the protocol */
	gdb_serve(0);
}

/* called after every m68k_execute slice in the emu loops */
void gdb_poll_stop(void)
{
	if (conn < 0) return;
	if (gdb_stop) { gdb_serve(1); return; }
	if (gdb_step) gdb_serve(1);      /* 's' ran one slice — report stop */
}

/* how many cycles to hand the CPU this slice (1 when single-stepping) */
int gdb_slice_cycles(void)
{
	return gdb_step && conn >= 0 ? 1 : 20000;
}

/* called from the instruction hook: breakpoint / first-step check */
void gdb_check_pc(unsigned pc)
{
	if (conn < 0) return;
	for (int i = 0; i < nbp; i++)
		if (pc == bps[i]) {
			gdb_stop = 'B';
			m68k_end_timeslice();
			return;
		}
}

/* called from mem.c write paths: write-watchpoint check */
void gdb_check_write(unsigned a, int size)
{
	if (conn < 0 || !nwp) return;
	for (int i = 0; i < nwp; i++)
		if (a < wp_hi[i] && a + size > wp_lo[i]) {
			gdb_stop = 'W';
			gdb_watch_addr = a;
			m68k_end_timeslice();
			return;
		}
}

int gdb_active(void) { return conn >= 0; }
