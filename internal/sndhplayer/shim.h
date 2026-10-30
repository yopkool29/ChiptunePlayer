/* C shim over the C++ AtariAudioRenderer API (cgo can't call C++
 * methods directly). */
#ifndef SNDH_SHIM_H
#define SNDH_SHIM_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct AAPlayer AAPlayer;
typedef struct {
	int subsongs, defsub;
	const char *title, *author, *year, *format;
} AAInfo;

/* open .sndh/.ym data (ICE!/LZH packing handled by the library);
 * returns NULL on unrecognized input. */
AAPlayer *aa_open(const void *data, uint32_t size, uint32_t rate);
void      aa_close(AAPlayer *p);
void      aa_info(AAPlayer *p, AAInfo *out);
int       aa_init_subsong(AAPlayer *p, int n); /* subsongs are 1-based */
uint32_t  aa_duration_samples(AAPlayer *p, int subsong);
void      aa_render_vu(AAPlayer *p, int16_t *buf, uint32_t n, uint32_t *vis);
void      aa_mute(AAPlayer *p, uint32_t mask);
void      aa_ffwd(AAPlayer *p, uint32_t n);

#ifdef __cplusplus
}
#endif
#endif
