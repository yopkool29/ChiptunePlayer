package deliplayer

import "testing"

// GUI regression: muted[] is [8]bool but a DeliPlayer has 4 channels.
// Applying the full array after a track load wrapped indices 4-7 back
// onto channels 0-3 via v&3, unmuting voices the user had silenced.
// MuteVoice must clamp to the real channel count.
func TestMuteNoWrap(t *testing.T) {
	testChdirRoot()
	PlayerDirs = []string{"internal/bundled/players", "internal/bundled/players-uade"}
	p, err := LoadFor("testdata/Art Of Noise/DSN/action section.aon", 0)
	if err != nil {
		t.Skip(err)
	}
	defer p.Close()
	// what applyLoadResult does with muted=[false,true,false,true,false...]
	for i := 0; i < 8; i++ {
		p.MuteVoice(i, i == 1 || i == 3)
	}
	buf := make([]byte, 44100*4/10)
	for i := 0; i < 15; i++ {
		p.FillBuffer(buf)
		if v := p.VoiceVolume(1); v != 0 {
			t.Fatalf("muted ch1 rendered audio: level=%d", v)
		}
	}
}
