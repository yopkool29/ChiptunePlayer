#include "shim.h"
#include "AtariAudioRenderer.h"
#include <new>

struct AAPlayer {
	AtariAudioRenderer *r;
};

AAPlayer *aa_open(const void *data, uint32_t size, uint32_t rate)
{
	AAPlayer *p = new (std::nothrow) AAPlayer;
	if (p && !(p->r = AtariAudioRenderer::Create(data, size, rate))) {
		delete p;
		return nullptr;
	}
	return p;
}

void aa_close(AAPlayer *p)
{
	if (p) {
		AtariAudioRenderer::Destroy(p->r);
		delete p;
	}
}

void aa_info(AAPlayer *p, AAInfo *out)
{
	const AtariAudioRenderer::SongInfo &i = p->r->GetSongInfo();
	out->subsongs = i.subsongCount;
	out->defsub = i.defaultSubsong;
	out->title = i.musicName;
	out->author = i.musicAuthor;
	out->year = i.year;
	out->format = i.fileFormat;
}

int aa_init_subsong(AAPlayer *p, int n)
{
	return p->r->InitSubSong(n) ? 1 : 0;
}

uint32_t aa_duration_samples(AAPlayer *p, int s)
{
	return p->r->GetSubsongDurationSample(s);
}

void aa_render_vu(AAPlayer *p, int16_t *buf, uint32_t n, uint32_t *vis)
{
	p->r->AudioRenderWithVisualInfos(buf, n, vis);
}

void aa_mute(AAPlayer *p, uint32_t mask) { p->r->MuteVoices(mask); }
void aa_ffwd(AAPlayer *p, uint32_t n)    { p->r->FastForward(n); }
