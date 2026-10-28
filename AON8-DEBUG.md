# AON8 (ArtOfNoise-8V) — silent playback investigation log

## RESOLVED — root cause chain (2025-10-28)

Three stacked bugs, all fixed:

1. **`hunk.c` tag inversion + early relocs.** `0x3EA` (DATA) was treated as
   BSS (skipped its payload) and `0x3EB` (BSS) fell into the unknown-tag
   branch, so hunk1/hunk2 were never loaded and RELOC32 was applied before
   the target hunks existed → every absolute reference to DATA/BSS stayed
   zero. Fixed by loading CODE/DATA properly, allocating BSS, and deferring
   relocations until all hunks are allocated.
2. **No audio DMA-end IRQ.** Streaming players feed the next sample chunk
   from the int4 handler on Paula audio-DMA completion. Added `INTREQ`
   set/read/ack in `paula.c` + `emu_aud_irq()` edge-triggered dispatch +
   `SetIntVector(4)` capture in `delihost.c`.
3. **Wrong frame for raw `$70` handlers.** The CIA-style stub calls the
   handler with `jsr` — fine for exec servers (rts). AON8 pokes `$70`
   directly: its handler ends with `rte`, which popped the jsr frame as a
   fake exception frame → PC=0. `emu_aud_irq` now builds a real
   `[SR][PC][format]` frame so `rte` lands on a lone `rts` (`IRQ_RTS`) that
   returns to CALL_DONE.

Result: `TestAONLoadRenders` peak=255, `TestAONLoadFor` picks
ArtOfNoise-8V end-to-end.

Kept for future debugging: `DELI_WATCH` now accepts ranges (`lo-hi`) and
also fires on `move.b` writes; `Player.emuRead`/`deliplayer_ram` RAM peek;
`DELI_TRACE` LOWJUMP hook.

Player: `internal/bundled/players-uade/ArtOfNoise-8V` (hunk binary)
Module: `testdata/Art Of Noise/Slaxx/dream01.aon` (`AON8ArtOfNoise` magic)

## Memory map (emulated addresses)

- hunk0 CODE base: `0x1022a0`  → emu_addr = file_off + 0x1022a0
- hunk1 DATA (4B): `0x108ac4`  ← Check2 writes module ptr here (emu `0x1023e4`: `move.l a0,$0.l` reloc'd)
- hunk2 BSS  (~1.5KB): `0x108ac8` ← DMA double-buffers (8 × 128B)
- vector 0x70 = int4 audio DMA-end handler → `0x10420e` (file `0x1f6e`)
- mixer voice structs (file-abs `$6444+0x24*i`): `0x1086e4`+ — fields:
  +0x0 stream ptr, +0x4 offset, +0x8 chunk len, +0xc next chunk ptr, +0x10 render-fn/buffer
- channel state structs (file-abs `$1ae6+0x62*i`): `0x103d86` — +0x2 stream ptr src,
  +0x1e period, +0x51 state byte (`0x21` seen written)
- voice-param table (file-abs `$6564`, 8×16B): `0x108804` — tick copies chan fields here
  (+0x0 = chan+2 stream ptr, +0x6 = period 0x0d60 seen)
- InitSound flag `$20c`: `0x1086ac` — ==1 confirmed (InitSound completes)
- 50 Hz tick: tag T_INTERRUPT → `emu_set_tick` — parser entry `0x108884` (file `0x65e4`)

## Established working chain (all confirmed via watches/traces)

1. Check2 → module ptr stored to hunk1 ✓
2. InitSound → fills DMA descriptors `0x1045c2` region → BSS buffers ✓
3. InitSound → `$20c`=1, INTENA=0xc780, DMACON=0x820f, AUDxLC/LEN=64w/PER=0xfa/VOL=0x40 ✓
4. Paula DMA-end → int4 edge-triggered → handler at `0x10420e` runs, INTREQR read ✓
5. 50 Hz tick → parser runs, both channel processors write state ✓

## Current bug

IRQ mixer loads `a4 = $10(a5)` (voice struct +0x10) → always 0 → LOWJUMP→0.
Voice structs `$6444`-family are ONLY ever *read* via `lea` in the IRQ handler;
file content is zero; no absolute store exists → they are filled via
register-indirect writes by some routine not yet identified.

Candidate: the pattern row decoder `jsr $3bc.l` (reloc'd → emu `0x10265c`,
file `0x3bc`) called by `0x108940` pattern-row routine — should fill chan+2
(stream ptr) which the 2nd tick processor then copies to the $6564 table.
Observed: chan+2 stays 0 → decoder either bails or writes elsewhere.

## Tooling

- venv capstone: `/tmp/venv68k/bin/python3`  (m68k, mode M68K_040, skipdata)
- code dump: `/tmp/aon8_code.bin` (hunk0 payload, 0-based file offsets)
- watchpoint env: `DELI_WATCH=a1,a2,...` (only wr16/wr32 hit it — `move.b`/wr8 blind)
- LOWJUMP hook: `DELI_TRACE=1` → prints when PC falls below 0x100 + regs + bytes
- PAULA_TRACE=1: AUDx/DMACON/INTENA writes
- host trace: `DELI_TRACE=1` → `[host] slot=..` / `[uade] lvo_idx=..`
- RAM peek: `Player.emuRead(addr,n)` Go test helper → `deliplayer_ram` C export

## Open questions / next steps

- [ ] Disasm row decoder at file 0x3bc: what register holds chan ptr, which
      fields written, what early-exit conditions (subsong? speed? gate flag?)
- [ ] Find ALL writers to $6444-family via reg-indirect: likely the tick's
      2nd processor chain or the mixer "next-chunk" producer
- [ ] Verify `uade.library lvo_idx=0` return contract (called during init)
- [ ] When voice+0x10 fills → confirm mixer writes non-zero into 0x108ac8
      buffers → Paula peak > 0 → flip aon_render_test to assert peak

---

# Postmortem — résolutions

## AON8 (dream01.aon) — RÉSOLU, peak=255

Trois bugs cumulés, dans l'ordre de découverte :

1. **`hunk.c`** : tag `0x3EA` (HUNK_DATA) traité comme BSS (payload jamais
   copié) et le vrai BSS `0x3EB` tombait dans la branche skip → parse cassé.
   + `RELOC32` appliqué à la volée **avant** l'allocation des hunks suivants
   → `base[hnum]==0` → relocs vers hunks tardifs silencieusement droppées.
   Fix : CODE/DATA chargés, BSS allouée, relocs collectées puis appliquées
   après le parse complet.

2. **IRQ de fin de DMA audio absente** : les players streaming (mixer
   logiciel) se ré-alimentent via l'IRQ audio Paula. Ajout : `INTREQ`/
   `INTREQR` émulés, `emu_aud_irq()` edge-triggered par canal à l'épuisement
   de `len`, `SetIntVector(4)` capturé, `a1=is_Data` passé via le stub.

3. **Frame `$70` incorrecte** : le handler int4 d'AON8 pique le vecteur
   `$70` directement et finit par `rte`. Le stub `jsr`/`rts` adapté aux
   handlers exec faisait dépiler une fausse frame → PC=0 → LOWJUMP.
   Fix : vraie frame d'exception `[SR][PC][format]` + `rts` sentinelle
   `IRQ_RTS` en cible de retour pour `rte`.

Tests : `TestAONLoadRenders` (peak asserté), `TestAONLoadFor` (probe E2E).
AHX non régressé (peak=11673).

## AON4 (action section.aon) — RÉSOLU, peak=255

Le player fonctionnait en fait correctement côté 68k : le tick CIA en 3
phases (state0 = row-parse + apply, state1 = DMACON différé, state2 =
reload loop `clr flag`) primait LC=`0x108054`, LEN=`0x5d5`, PER=`0xca`,
DMACON=`0x800f` — tout nominal.

**Coupable : écriture byte du volume.** `ArtOfNoise-4V` écrit AUDxVOL via
`move.b d0,$9(a5)` → store byte à `$dff0a9` (adresse **impaire** = octet
bas du registre word). `wr8` appelait `paula_write(0xa9)` → `off=0x09` sans
case dans `chan_write` → drop → `p->vol=0` à vie → silence.

Fix : `paula_write8()` — sémantique complète des écritures byte sur regs
16-bit (adresse impaire = octet bas, paire = octet haut) pour les regs
canal + merge dans les shadows DMACON/INTENA/INTREQ/ADKCON. `wr8` routé
dessus dans `mem.c`.

Tests : `TestAON4` (peak + tickHz assertés), `TestAON4LoadFor`.

## AON4 lent (3× trop lent) — RÉSOLU, tick 150 Hz

Symptôme utilisateur : "les musiques sont lentes (.aon)" après le fix
audio. Le son jouait mais ~3× trop lent.

**Chaîne causale (deux bugs combinés) :**

1. **CIA-B timer B non émulé.** `ArtOfNoise-4V` ouvre `ciab.resource` et
   appelle `AddICRVector(d0=1)` = **timer B de la CIA-B**, puis programme
   lui-même le latch : `move.b #$79,$bfd600` + `move.b #$12,$bfd700` →
   latch `0x1279` = 4729 E-clocks = **150 Hz**, puis `bset #0,$bfdf00`
   (CRB start). Sa routine tempo : `latch = 0x9052D / tempo`
   (0x9052D = 1773447/3 — le facteur 3 est sa machine à 3 phases :
   interrupt à 150 Hz ÷ 3 phases = row clock à 50 Hz). Notre modèle ne
   connaissait que CIA-A timer A → écritures `$bfd*` droppées, tick
   resté au défaut `dtg_Timer` 50 Hz → parser à 16,7 Hz au lieu de 50.
2. **`AddICRVector` retournait le mask au lieu de NULL.** Le player
   teste d0 après l'appel (`bne` → chemin d'erreur `return -1`) : notre
   retour `d0=1` le faisait passer en échec → il **skippait** ses propres
   écritures de latch et le start — la preuve : les `move.b $bfd6xx`
   post-`jsr -6(a6)` n'apparaissaient jamais dans les traces. Autodoc :
   "If it succeeds, a null is returned".

**Fix :**
- `cia.c` : deux chips × deux timers (`latch[chip][tim]`, regs CIA
  standards : 4/5=TALO/TAHI, 6/7=TBLO/TBHI, 0x0E=CRA, 0x0F=CRB) ;
  `cia_set_icr(chip,tim)` mémorise quel timer porte le vecteur capturé ;
  `cia_tick_hz()`/`cia_pump()` suivent ce timer ; `ciab_write/read`.
- `delihost.c` : `AddICRVector` appelle `cia_set_icr(a6==g_lib_ciab,
  d0==1)` et retourne **0** ; `OpenResource` rend `g_lib_ciab` quand le
  nom contient "ciab" (avant : toujours ciaa) ; `S_SETTIMER` → `cia_arm`
  (arme le timer du vecteur, pas forcément timer A).
- `mem.c` : routage `$bfd000-$bfdfff` → `ciab_read/write`.

**Vérifié** : `deliplayer_tickhz` = **150.0 Hz** après load (assertion
dans `TestAON4`), compteur de vitesse ×3, suite deliplayer verte,
AHX non régressé. -8V reste à 50 Hz (son design : parser à chaque tick,
streaming piloté par l'IRQ audio int4).

À retenir : `AddICRVector` retourne NULL au succès ; et tout player peut
reprogrammer le latch CIA de SON timer — le tick de rendu doit suivre
les écritures de regs, pas seulement `dtg_Timer`.

## Cartographie AON4 (retenue pour la suite)

- hunk1 (2e CODE) base `0x102690`, state struct `a6=0x103fd0`
- `$243(a6)`=0x104213 : phase machine (0 travail → 1 DMACON → 2 reload)
- `$244(a6)`=0x104214 : shadow DMACON
- table canaux `0x104216`, stride `0x62`, 4 voix
- flag `chan+0` : 3 = note armée (bit1 = "primer LC"), clr par le reload
- `chan+2` ptr sample, `+6` len, `+a` ptr loop (`0x1036ce` = silence:
  `ori.b #0,d0` ×n), `+e` len loop, `+14` vol, `+1c/+1e` période
- routine A `0x103626` (apply AUDx), routine B `0x1036ac` (reload loop)
- row parser `0x102986`, per-chan effects `0x1030de`, trigger `0x102d60`

## « Souffle » / parasitage en fin de note — RÉSOLU (confirmé à l'oreille)

Deux bugs dans `paula.c`, indépendants du player :

1. **Byte fantôme répété** : quand un échantillon non-bouclé s'épuisait
   (`len==0`, `llen==0`, ou `ptr==0`), `next_byte` renvoyait 0 mais
   `have_w`/`w`/`half` restaient figés → `paula_render` réémettait le
   dernier byte entre les fetches → signal carré résiduel à la fréquence
   du period, au volume du canal. Fix : `have_w = 0` dans les deux sorties
   "plus rien à jouer".

2. **Re-trigger depuis une position périmée** : au front montant d'un bit
   canal de DMACON, le canal repartait de son `ptr`/`len` figés au moment
   du stop (queue du vieux sample). Le vrai hardware reverrouille
   AUDxLC/LEN au DMA-on. Fix : rising edge → `ptr=loop`, `len=llen`,
   `half=frac=have_w=0`.

**Séquence AUDx de -4V** (trace PAULA_TRACE, wings of death 5) : PER ×4 →
DMACON no-op → LC/LEN par routine B à chaque tick phase-2 — le player
entretient le reload-latch en permanence, la double-bufferisation réelle
fait le reste. Aucun write AUDxDAT.

Trace de debug conservée : `PAULA_TRACE=1` → DMACON transitions, rising
edges (ptr/len/vol/per latchés), drains→reloads, mutes — horodatés en
temps-ému.
