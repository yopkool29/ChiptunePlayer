//go:build windows

// UADE is not ported to Windows: libuade/uadecore rely on fork() and
// POSIX IPC, and ship here as Linux binaries. LoadFor always fails so
// the backend chain falls through to the native decoders/deliplayer.
package uadeplayer

import "errors"

var errNoUADE = errors.New("uadeplayer: not supported on windows")

// SetBaseDir is a no-op: there is no UADE runtime dir on Windows.
func SetBaseDir(string) {}

// Player is never instantiated — LoadFor always returns an error.
type Player struct{}

// LoadFor implements the same entry point as the POSIX build.
func LoadFor(string, int) (*Player, error) { return nil, errNoUADE }

func (p *Player) Load(string, int) error            { return errNoUADE }
func (p *Player) Reinit(int) error                  { return errNoUADE }
func (p *Player) Close()                            {}
func (p *Player) FillBuffer(b []byte)               { for i := range b { b[i] = 0 } }
func (p *Player) SongEnd() bool                     { return true }
func (p *Player) Duration() uint32                  { return 0 }
func (p *Player) Songs() int                        { return 0 }
func (p *Player) FormatID() string                  { return "uade" }
func (p *Player) FormatName() string                { return "" }
func (p *Player) Name() string                      { return "" }
func (p *Player) Artist() string                    { return "" }
func (p *Player) Title() string                     { return "" }
func (p *Player) Game() string                      { return "" }
func (p *Player) Voices() int                       { return 0 }
func (p *Player) VoiceVolume(int) int               { return 0 }
func (p *Player) SetPath(string)                    {}
func (p *Player) MixerInit(int, int, int, int, int) {}
func (p *Player) MuteVoice(int, bool)               {}
func (p *Player) SetLoopMode(bool)                  {}
func (p *Player) SetFiltering(bool)                 {}
func (p *Player) SetPanning(int)                    {}
func (p *Player) Seek(int)                          {}
func (p *Player) EndShorts(bool, int)               {}
