# Release readiness

Updated October 27, 2026. This is the single current release checklist.
[GOAL.md](../GOAL.md) defines the first controlled release's scope. Historical
reports below and in [production-progress.md](production-progress.md) retain
their original failures and package-specific results; they are not a list of
current runtime defects.

## What is already working

- One Factorybox codebase implements the same lifecycle for Linux AMD64 and
  ARM64. CPU-specific restore/timer code stays behind that shared lifecycle.
- The final local ARM64 Factory integration uses worker
  `factorybox-build-worker-008d3ab940204c87a01befa6ca268a8f`, executable
  `431fb101edd1d62ccf67b9cb0fc22b52e663606860426b61d0f3b92658727b4d`,
  and reviewed Factory commit `a5b9a931fa2a61e328d122f6ce1250664b52459e`.
  The IDE proof passes 105 assertions. The clean Staging-derived paused Fork
  pool, automatic demand/resume and memory release have native observations.
  The pool command's cleanup-only failure and later exact reconciliation
  remain explicit in the [consumer evidence](factory-consumer.md).
- The intermittent post-detach Resume refusal and interrupted unpublished BPF
  load were fixed in `e12f18d` and `55bd66e`, with targeted native regressions.
  The final worker passes twelve consecutive diagnostic Pause/Resume cycles.
  These are functional results, not full release qualification.
- The Systemd proc-security fixture now observes the actual kernel capability
  before Sysbox starts. An existing kcore must deny reads with the original
  assertions; an absent kcore must remain absent in the guest and fail an
  attempted read. Kmsg denial, zeroed kallsyms, all test titles and the original
  skip set are preserved. The source lock declares the ordered patch and exact
  original, preceding and resulting hashes. The complete native ARM64 Systemd
  suite now passes in `factorybox-bats-f75e703b59ed495bb30b55647e78dc82`:
  four executed passes, including proc exposure, and the same two upstream
  skips. Cleanup, unchanged components/inputs and unchanged observed global
  sysctls all pass. The old failed run is preserved, not reclassified.
- The launcher detects the selected Linux Docker server, whether launched from
  Linux or macOS. Release CI has matching AMD64 and ARM64 native jobs, the
  reviewed Factory pin, distinct architecture assets and a combined manifest
  gated on both jobs. Editing CI does not mean either job has already passed.
- Native AMD64 verification now passes all 23 required release gates for source
  `955446f1aa9630a9692de86dc8568292bc401647`, ordinary worker image
  `sha256:073ce8ad27c63ed5261a4da95c4e9349cc81ac208b33abafa639afa5b8c9b9dc`,
  and reviewed Factory consumer `a5b9a931fa2a61e328d122f6ce1250664b52459e`.
  The complete receipt is
  `factorybox-release-native-c627416461824f56a49d96160e4b4b87`, SHA-256
  `d0c2bd0100f2999015548416aba2e21a41944089eb6ca4304a5696532e9a54a8`.
  This includes both worker-replacement modes and both Factory consumer lanes.
  Fix `884e613` removes only unused DWARF from the capacity fixture's guest
  executable; all native assertions, store limits and delivered runtime bytes
  remain unchanged. The original failed matrix
  `factorybox-release-native-8463fd1f2f4345ed8c1945f7296b3ed9` and both reproduced
  capacity failures remain preserved. Cleanup removed the owned test resources
  and builder; all nine pre-existing volume identities are retained.
- The verified fix and AMD64 report are on `main` at `7c1e53b`.
  [Both portable CI jobs passed](https://github.com/maxcodefaster/factorybox/actions/runs/40584615545)
  there. The subsequent `0.1.0` preparation fixes image reproducibility: Docker
  had timestamped the missing `/opt` parent differently despite identical
  packages. Both fixed no-cache builds, `factorybox-build-worker-7e0a025ca2454077a91404e2ca64da53`
  and `factorybox-build-worker-90f6cc09046a4ff2a153c2322fb27276`, produce image
  `sha256:b70c858c6c8a918f1fca3259cd469c9f53a85d1a4e5be12723dd6e762a689ebf`
  and package `da9b6b511e5a077bfa7a80586b3202ebbe89b267809790ec8eb30e6593a2d80e`.
  All seven local portable gates pass with 409 Python tests. The original failed
  independent build `factorybox-build-worker-acc4fdca454e480f9f048904de9bc50a`
  stays failed and preserved.
- The ordinary `0.1.0` AMD64 image above now passes all 23 native release gates
  on source `9a17ba720d3b03f0c2fddc785ae2190542fb3807`, with executable
  `d7734394fb92d333c3e08a11dbc8e24da2bc3e6b50ac7b940fe69a5ee34e8247`
  and the same reviewed Factory pin. The exact receipt is
  `factorybox-release-native-dcb1e1c8db0a4f4ebf8f05065848bbc6`, SHA-256
  `1e804b500d42f8095ed7443d4427c041a22e0e6c4752460ca79ddf0187dfdfe9`.
  Independent compilation also reproduces Sysbox and CRIU. Every native child
  confirms cleanup; the temporary builder is removed and all nine retained
  volume identities remain unchanged. The earlier headroom-refused attempt
  stays failed. The [AMD64 report](native-amd64-0.1.0.md) records identities,
  limitations, resource handling and evidence locations.

- Pre-release review then identified the transient seccomp `POLLERR` close
  behavior. Ordered patch `sysbox-fs-seccomp-poll-retry.patch` backports upstream
  `1ac3fd588495674eb8604f3c79b1573348eb3487` with attribution and all six new
  regression cases. Original assertions and vet remain intact. The controlled
  old-decision test fails specifically on the transient event; the backport
  passes. This does not claim a spontaneous kernel-level reproduction.
- The updated ordinary AMD64 image now also passes all 23 native gates on
  source `1023be56dd70ed2115b1125c122a4f056b7824f8`: image
  `sha256:b325efac164561af8abd74932b2d0e44633fed613d44c7dba6b574235db34be8`,
  unchanged CLI `d7734394fb92d333c3e08a11dbc8e24da2bc3e6b50ac7b940fe69a5ee34e8247`,
  and the reviewed Factory pin above. The receipt is
  `factorybox-release-native-b6664d353f744f1dbbf6a87049115afa`, SHA-256
  `1167019e9903dd203b6a9cc3f8f80041dd8c53e03eb9d51c9e94aa75b0dd174e`.
  Independent builds reproduce the updated Sysbox executables, package and
  complete image. Seven portable gates pass with 409 Python tests. Cleanup
  confirms zero containers and all nine retained volume identities. The
  [AMD64 report](native-amd64-0.1.0.md#sysbox-fs-poll-retry-follow-up) records the
  new identities and retained fixture failures.

## Remaining first-release checks

- [ ] Complete the `0.1.0` ARM64 native matrix and bind both architectures' sources,
  component identities, independent builds and receipts in the final profile.
  The exact `0.1.0` AMD64 matrix above is complete. It does not qualify ARM64.
  Older local ARM64 integration results do not replace the image-specific
  checks or final profile binding. Each architecture needs an existing matching Linux
  runner with an admitted kernel/storage tuple. This session currently has only
  the AMD64 Docker host; GitHub has no registered native release runners.
- [ ] Resolve the historical EBUSY cleanup attribution. Later controls passed;
  the latest image has not been shown to reproduce it. The original holder and
  whether it belonged to the fixture or runtime remain unestablished. Do not
  call it a current reproduced runtime bug or silently mark it fixed.
- [ ] Complete the eight real-provider continuation cases required by the
  original controlled-release scope. A fresh October 27 availability probe with
  pinned OpenCode 2.0.27 confirms the same free model and pricing, then receives
  HTTP 429 before actual tool start. Its one request, response and confirmed
  cleanup are preserved in `factorybox-external-opencode-a04b67153f7d4a068e8de1194eadfe85`.
  The new retry-after lower bound is `2026-10-28T00:00:00.533718Z`; it does not
  guarantee availability. No failed case is retried automatically, and no model,
  identity or cost class is changed. The probe passes none of the eight cases.
- [ ] Review and select the final qualified profile before any activation or
  publication. Finish the upstream review and exact image's distribution/source
  obligations described in [LICENSING.md](../LICENSING.md). The component source
  inventory is verified; it does not cover every operating-system package's
  distribution obligations. Stable release preparation is authorized; no release
  or image has been published and no installation has been activated.

The 24-hour soak, 100-run series, additional optimization and automatic
retention remain deferred. Cross-node migration and arbitrary Unix/Linux hosts
are not added to this release. Production qualification remains false.

## Archived October 25 AMD64 report

Historischer Stand: 25. Oktober 2026. Die Freigabe war offen. Verbindlicher Umfang bleibt
[GOAL.md](../GOAL.md), einschließlich aller dort genannten Grenzen.
Der 24-Stunden-Dauerlauf und die 100er-Serie gehören zur späteren Etappe.

Die [gemessenen Laufzeiten](first-release-performance.md) fassen die vollständige
20er-Serie mit Vorbereitung, erster nutzbarer Arbeit und verbleibendem
Worker-Speicher zusammen.

Die reguläre Serie ist vollständig bestanden und unabhängig geprüft: zehn
Minimal- und zehn Factory-Läufe, ohne Fehlschlag, Wiederholung oder verworfenen
Versuch. Der ursprüngliche Ablauf ist beendet. Das vollständige Ergebnis steht
in `artifacts/factorybox-first-release-ordinary-series-97f5b8e9af9846f7b00f34e793dbf6d3/result.json`.
Die fünf Betriebsfälle sind anschließend vollständig bestanden und unabhängig
geprüft: Startfehler, Speicheridentität, alle acht Stop-Schutzfälle, der gelieferte
Compose-Ablauf und tatsächlicher Update/Rückfall. Auch alle fünf weiteren
Speicherfälle sind bestanden und unabhängig geprüft. Jetzt sind auch alle vier
zusätzlichen Drain-Fälle und beide ursprünglichen CLI-Worker-Verlustarten
bestanden und unabhängig geprüft. Beide lokalen Gateway-Kontrollen sind ebenfalls
bestanden und unabhängig geprüft. Offen bleiben die acht echten Anbieterfälle,
die historische Busy-Mount-Zuordnung und das endgültige Freigabeprofil.
Ein eingereihter Test ist kein Pass.

Der erste echte Anbieterfall ist inzwischen am kostenlosen Tageskontingent
gescheitert. Der Anbieter hat die tatsächliche Anfrage mit 429 abgewiesen,
bevor das Werkzeug startete. Damit sind null von acht echten Anbieterfällen
bestanden. Die gespeicherte Frist erlaubt einen weiteren Versuch frühestens
am 26. Oktober um 00:00:00.194689 UTC; das garantiert keine Verfügbarkeit.

Der Aufräumvergleich mit verzögertem Prozessende besteht in beiden Varianten mit jeweils 13
Haupt- und 65 Untertests. In der zweiten Variante warten 28 echte Helfer nach
ihrer Antwort mindestens 1,9 Sekunden länger bis zum beobachteten Prozessende.
Alle acht ursprünglichen unmittelbaren Unmounts je Variante gelingen. Die
unvollständige Root-/Arbeitsverzeichnis-Beobachtung liefert dabei keinen
überlappenden Besitzer. Der historische Fehler bleibt ungeklärt. Noch längere
Verzögerungen sind durch diesen Versuch nicht begründet. Der unabhängige
Nachweis liegt im Handoff unter `agent-exit-delay/root-window-json-closure.json`.
Ein Fehler des ersten Auswerteprogramms bleibt als negativer Gesamtstand
erhalten; seine JSON-Schlüsselkorrektur ändert keine nativen Testbedingungen.

Eine zusätzliche Kernel-Kontrolle belegt inzwischen einen blinden Fleck:
Ein gebundener Unix-Socket kann einen Mount halten, obwohl Datei- und
Arbeitsverzeichnis-Beobachter keine passende Referenz zeigen. Der ergänzte
Beobachter besteht alle fünf bisherigen und beide neuen Kernel-Kontrollen.
Eine vollständige Wiederholung des ursprünglichen Testprogramms besteht mit
17 Haupt- und 78 Untertests. Alle 17 direkt beobachteten Socket-Bindungen
werden freigegeben; ihre Mount-Objekte unterscheiden sich von den 15
ursprünglichen Aufräumzielen. Es gibt weiterhin keine Reproduktion oder
Ursachenzuordnung. Der Nachweis liegt im Handoff unter
`socket-path-reference/original-replay-closure.json`. Kopierte Pfadreferenzen
akzeptierter Sockets werden durch die Bindungsbeobachtung nicht eigenständig
erfasst. Weitere erfolgreiche Wiederholungen würden diesen offenen Punkt
allein nicht schließen.

Für die Abnahme verwenden wir den korrigierten Worker
`6016296965364344a8ef9eede420dbc8`, unabhängig identisch nachgebaut als
`829faef7ea3143038bebf72493474070`. Paket, Image und jede Image-Schicht stimmen
überein. Paket-SHA-256:
`3a33f704823e748395b2937b9f4d65fdfd6224ebc9b6ba165b4be115af7f0f5d`.
Er enthält die unabhängig reproduzierte CRIU-Komponente `a93016e0...` und
die unveränderten Sysbox-Komponenten `a3d550a7...`. Die vollständige Abnahme
dieses Pakets ist noch offen. Ältere Paketergebnisse ersetzen sie nicht.

Die beiden zuletzt gefundenen Fehler sind korrigiert und gezielt geprüft:

- CRIU meldete eine unvollständige Prozesssicherung fälschlich als Erfolg.
  Fehlerweitergabe und Wiederherstellung des betroffenen beendeten Prozesses
  mit seinem ursprünglichen Elternprozess, beiden PID-Ebenen und Exit-Status
  sind korrigiert. Der echte Linux-Gegenbeweis, sieben Fehlerweitergabe-Fälle
  und alle 18 unveränderten CRIU-Kernfälle mit je zwei Zyklen bestehen.
- Die Go-Abschlussprüfung verweigerte eine erfolgreiche Wiederherstellung
  bei einem kurzen Linux-Freezer-Übergang nach dem Lösen des Tracers.
  Derselbe echte Regressionstest scheitert vor der Korrektur und besteht
  danach mit 256 Zyklen und drei Verweigerungskontrollen. Der vollständige
  Pause-Test besteht mit 12 Haupttests, 67 Untertests und allen 16 ursprünglichen
  Restore-/Löschzyklen. Die Korrektur schreibt den Freezer nicht um.

Der ursprüngliche vollständige Factory-Fehlerfall besteht jetzt auch mit dem
neuen Paket in `factorybox-consumer-worker-loss-77412471c65b4ca988471b01cebe5c28`:
harter Worker-Verlust, tatsächlicher Platzmangel, erhaltene alte Pause,
erhaltener Teilimport, exakter Abbruch und Replay, Loop-Wechsel, unabhängige
neue Pause, Stilllegung der alten Epoche, nützliches zweites Resume und
anschließender Checkpoint/Fork mit unabhängigen Nachkommen. Aufräumen und
unveränderte globale Einstellungen sind bestätigt. Die erste nutzbare
wiederhergestellte Antwort benötigt in diesem Einzellauf 51,29 Sekunden.
Das ist keine Latenzverteilung oder Garantie. Die ursprünglichen negativen
Ergebnisse, Quellen und Fehlerdiagnosen bleiben erhalten. Das inzwischen
ersetzte vollständige Store-Abbild `ad6530e1...` wurde nach vollständiger
Entpackungs-/Hashprüfung und bestandenem identischem Gesamtfall ausgemustert.
Seine 612 bereits gesicherten Diagnose-Auswahlen sind erneut geprüft; der alte
gesamte Store ist ausdrücklich nicht mehr rekonstruierbar. Der Beleg liegt im
Handoff unter `resolved-pressure-retention/closure.json`. Andere ausgewählte
Eingänge und die weiterhin unaufgelösten vollständigen Fehler bleiben erhalten.
Damit ist die vorgeschriebene Reserve für neue native Tests wieder verfügbar.

Alle 17 Hauptgruppen für Go-Laufzeit und Speicher bestehen auf dem neuen Kern.
Der gemeinsame Nachweis
`factorybox-current-main-native-closure-9c499b4ac32a4ac0824da84515a910ce`
prüft eingefrorene Quellen, Testeingaben, benannte Ergebnisse, vollständige
Capture-Archive und das Aufräumen erneut. Die bytegleiche CRIU-Kernprüfung
mit 18 Fällen und je zwei Zyklen ist gesondert gebunden.

Zusätzlich bestehen inzwischen die vollständige Stilllegungssuite mit allen
acht ursprünglichen Haupttests in `67825546a2e747e784a836d8008ab4d2` und der
gezielte Kapazitätsfall in `efea9fa67a1f48688f539c240d94c84f`. Beide verwenden
das aktuelle Worker-Image und bestätigen unveränderte Eingaben und Aufräumen.

Auch die elf noch offenen gezielten Timer-, Restore-Hold- und Metadatenfälle
bestehen jetzt in
`factorybox-current-targeted-native-gates-1d4dba81823b45019d0d37e747face60`.
Der vollständige Metadatenfall verwendet drei echte bestätigte Pausen und
200 öffentliche Basisimporte, anschließend sauberes Stoppen beider Worker
und nützliche Wiederherstellung in einer neuen Epoche. Der Timer-Abbruchfall
erzeugt tatsächlichen Platzmangel und prüft die ursprüngliche Anwendung danach.
Die unabhängige `closure.json` prüft alle elf Ergebnisse sowie die bereits
bestandenen Stilllegungs- und Kapazitätsfälle erneut. Damit sind in diesen
13 Gruppen 20 ursprüngliche Haupttests belegt, ohne reduzierte Metadatenfälle,
Prototypen oder Wiederholungen fehlgeschlagener Läufe.

Beide vollständigen nativen Epochenfälle bestehen ebenfalls auf diesem Paket:
`factorybox-bounded-loss-ca6d142979f94db4b96569fe99a55ef4` mit geplantem und
`factorybox-bounded-loss-e9d74c5b0e384ada80deabab359dc019` mit hartem ursprünglichem
Worker-Verlust. Beide unterbrechen zusätzlich die akzeptierte Stilllegung,
wechseln den Worker und das Loop-Gerät und schließen die exakte Operation mit
nützlicher unabhängiger Nachfolge-Sicherung ab. Die ursprünglichen
Platzmangel-, Besitz-, Fremdeintrags- und Abbruchprüfungen bleiben erhalten.

Die vollständigen Anwendungstests bei tatsächlichem Platzmangel bestehen nun
für alle vier Kombinationen: Factory und Minimal jeweils nach geplantem und
hartem Worker-Verlust. Die drei neuen Fälle liegen in
`factorybox-current-pressure-consumers-5f8ed07f5393427abba34b1d93973935`.
Dessen unabhängige `closure.json` bindet auch den früheren harten Factory-Fall,
die vollständig geprüften Eingaben, die ursprünglichen Assertions und das
Aufräumen. Diese Fehlerfälle zählen nicht zur gesonderten regulären 20er-Serie.

Die unveränderten Sysbox-Komponenten sind außerdem erneut an das aktuelle
Paket gebunden. Dabei sind die vollständigen ursprünglichen Unit-/Bats- und
beiden Komponenten-Integrationsnachweise samt ursprünglichen Skips geprüft.
Die aktuelle CRIU-Kernprüfung bleibt gesondert an ihre korrigierten Bytes
gebunden. Alte Go-Laufzeitergebnisse werden damit nicht übernommen.

Jetzt sind alle 15 benannten Integrationsfälle nachgewiesen. Die fünf
Retained-Fälle, sechs Docker-Abbruchfälle und beiden vollständigen
Anwendungsfälle bestehen auf dem aktuellen Kern beziehungsweise der
ausgelieferten CLI. Die absichtlich beschädigte RAM-Sicherung wird durch die
ursprüngliche Integritätsprüfung abgelehnt; ihr negativer Einzeltest bleibt
unverändert erhalten. Der gemeinsame unabhängige Nachweis liegt in
`factorybox-current-integration-final-2debe360d2724cff957b10e4a2319a5a/closure.json`.
Er bindet auch die beiden unveränderten Sysbox-Integrationen mit ihren
ursprünglichen Skips. Zwei vorherige Abläufe hielten vor dem nächsten Test
an der Speicherreserve; ihre negativen Gesamtstände bleiben erhalten.
Bereits bestandene Fälle wurden nicht erneut ausgeführt. Diese Prüfungen
zählen weiterhin nicht zur regulären 20er-Serie.

Die Serien `93d21ac9...` und `a3d09ea4...` hielten nach fünf beziehungsweise
acht weiteren Erfolgen vor der nächsten Gruppe an der Artefaktreserve an.
Diese Gruppen hatten noch nicht begonnen. Beide negativen Gesamtstände
bleiben erhalten. Die letzte Erklärung
`factorybox-current-native-final-b19c38f289cd492084650d66423f79a0`
übernimmt die 13 geprüften Erfolge, führt nur die drei offenen Gruppen aus
und besteht vollständig. Zusammen mit dem vorherigen vollständigen Pause-Test
sind damit alle 17 Hauptgruppen belegt. Die reguläre 20er-Serie ist unter der
unten benannten neuen Erklärung vollständig bestanden und unabhängig geprüft.

| Anforderung | Belegter Stand des aktuellen Kandidaten | Noch erforderlich |
| --- | --- | --- |
| Paket reproduzieren | 6016 und 829f sind identisch. | Endgültiges Abnahmeprofil mit allen Ergebnissen verbinden. |
| Native Laufzeit, Speicher und Stilllegung | Alle 17 Hauptgruppen, elf weitere gezielte Timer-/Restore-Hold-/Metadatenfälle, die vollständige Stilllegungssuite, Kapazität und beide unterbrochenen nativen Epochenfälle bestehen; CRIU-Kernprüfung ist bytegleich gebunden. Alle fünf zusätzlichen Speicherfälle einschließlich beider Abschlussgrößen-Grenzfälle mit erklärter Altbestand-Fixture sind bestanden und unabhängig geprüft. | Die Nachweise im Endprofil verbinden. |
| Vollständige Integration | Alle 15 benannten Fälle sind unabhängig geprüft: fünf Retained-, sechs Docker-Abbruch-, zwei vollständige Anwendungs- und zwei unveränderte Komponentenfälle. Die erwartete Ablehnung beschädigten RAMs und ursprünglichen Upstream-Skips bleiben ausgewiesen. | Nachweise im endgültigen Abnahmeprofil verbinden. |
| Anwendung bei vollem Speicher wiederherstellen | Alle vier vollständigen Factory-/Minimal-Fälle bei geplantem und hartem Worker-Verlust bestehen auf 6016, einschließlich Platzmangel und Epochenstilllegung. | Nachweise im endgültigen Abnahmeprofil verbinden. |
| Ursprüngliche 8-GiB-Sicherung weiterbenutzen | Bestanden in `factorybox-enospc-original-rescue-e66b1b1a3bdb4cf39d49f6c67d6eb13d`: unverändertes schreibgeschütztes Original auf Loop0, unabhängiger Import und echte Wiederherstellung mit dem ursprünglichen CLI-/CRIU-Rezept. Vollständiger ursprünglicher Anwendungszustand und gespeicherter Agentenabbruch stimmen überein; eine neue Anwendungsänderung besteht ohne Werkzeug-Replay. Originalabbild unverändert, Aufräumen vollständig, insgesamt 93,33 Sekunden. Die tatsächlichen Dienstpfade des frischen Workers mussten dem gespeicherten Rezept entsprechen. | Nachweis ins Endprofil aufnehmen. Dieser forensische Weg qualifiziert weder das alte Paket als Rückfallversion noch einen Rezeptwechsel für alte Sicherungen. |
| Alten Busy-Mount-Fehler erklären | Helferverzögerung und abgeschaltete GC-Auslöser der Originaldienste sind geprüft. Beide vollständigen ursprünglichen Prüfsequenzen mit je 29 Haupttests und 112 Untertests bestehen ohne EBUSY. Die Verzögerung belegt jedoch keinen tatsächlich gehaltenen Dateisystemverweis beim entscheidenden Aushängen. Die ursprünglichen Löschbelege und die auch in bestandenen Läufen auftretende Dienstwarnung sind gesondert geprüft. | Den tatsächlichen Besitzer am unveränderten Aushängeschritt beobachten und den Fehler Test oder Laufzeit zuordnen; nötige Korrektur belegen. Weitere identische erfolgreiche Verzögerungsläufe schließen diese Messlücke nicht. |
| Echten externen Agenten fortsetzen | Die erste echte Anfrage nach der ursprünglichen Wartefrist wird am 2026-10-25 erneut mit HTTP 429 / FreeUsageLimitError abgelehnt. Der vollständige Factory-/Theia-Fall schlägt vor dem tatsächlichen Werkzeugstart fehl; die Folge stoppt. Null von acht Anbieterfällen sind bestanden, die weiteren sieben wurden nicht gestartet. Fehler, vollständiger Store, kostenlose Modellzulassung und exaktes Aufräumen sind unabhängig gebunden. | Beide nativen Gateway-Kontrollen sind abgenommen. Alle acht vollständigen echten Anbieterfälle bleiben erforderlich, sobald das kostenlose Kontingent verfügbar ist. Die neue gespeicherte Untergrenze ist `2026-10-26T00:00:00.194689Z`; sie garantiert keine Anbieter-Verfügbarkeit. Kein Wechsel von Modell, Identität oder Kostenklasse und keine automatische Wiederholung des Fehlversuchs. |
| Update, Drain und Rollback prüfen | Alle fünf Betriebsfälle sind auf 6016 bestanden und unabhängig geprüft, einschließlich aller acht Stop-Schutzfälle, Compose und tatsächlichem Update/Rückfall. Auch beide Verbraucher mit/ohne erzwungenen Loop-Wechsel beim Drain-Abbruch sowie beide ursprünglichen CLI-Worker-Verlustarten sind bestanden und unabhängig geprüft. Der Rezeptwechsel leert kontrolliert; falsches Rezept wird verweigert und die aktuelle Pause mit dem richtigen Paket wiederhergestellt. | Nachweise ins Endprofil aufnehmen. Die alten Pakete sind keine qualifizierten Rückfallversionen. |
| Reguläre Wiederholungsserie | Alle 20 vorab festgelegten vollständigen Läufe auf 6016/829f sind bestanden und unabhängig geprüft, je zehn Minimal/Factory. Befehle, Reihenfolge, Originaltests, Eingaben, Ressourcen, Speicherfreigabe und vollständiges eigenes Aufräumen stimmen. Frühere Serien behalten null Läufe; Fehlerfälle zählen nicht rückwirkend mit. | Vollständigen Nachweis im endgültigen Abnahmeprofil verbinden. |

Die [Fortschrittsbelege](production-progress.md) enthalten die einzelnen
Artefakte, Fehler und Ersetzungen. Die
[Betriebsanleitung](first-release-operations.md) bleibt bis zur vollständigen
Abnahme eine Anleitung für den geprüften Entwicklungsstand.

Die frühere Aussage, dass das Originalabbild zunächst zusätzlichen Platz im
Artefaktbudget benötige, war ein Fehler im privaten Vorbereitungscheck.
Temporäre Docker-Stores werden wie in den bestehenden nativen Tests gegen den
freien Host-Speicher geprüft. Das 32-GiB-Artefaktlimit, die 8-GiB-Startreserve
und der freie Host-Mindestbestand bleiben unverändert. Neue fehlgeschlagene
Versuche erzeugen allerdings echte zusätzliche Belege im Artefaktverzeichnis.

Die vorherige, an ein älteres Paket gebundene Serienerklärung liegt unter
`artifacts/factorybox-first-release-ordinary-series-c5663e4172ef495485621d537afbbef9/plan.json`,
SHA-256 `ec9cbaa19c68ce0e22b6587fa827de5fe49bf3131fd5de2422c5cd72c559608d`.
Sie und alle Vorgänger bleiben unverändert mit null ausgeführten Läufen.
Die neue Erklärung für 6016/829f liegt unter
`artifacts/factorybox-first-release-ordinary-series-97f5b8e9af9846f7b00f34e793dbf6d3/plan.json`,
SHA-256 `8f7d6f1f0724845f696009120008fc5535aee38c9f31a1aab768eaac571f7edd`.
Sie erhält alle 20 ursprünglichen Befehle, Prüfungen, zehn Läufe je Beispiel,
den vollständigen 420-Sekunden-Lifecycle und die unveränderte Startreserve.
`progress.json` und der abschließende `result.json` führen jeden Versuch unter
seiner vorher festgelegten Nummer. Ein gestarteter Lauf wird bei unklarem
Beobachtungsstand nicht neu gestartet.

Die eingereihten Abnahmen sind vor ihrem Beginn festgelegt:

- Betrieb: `factorybox-current-operations-plan-541b0fd1630949d3a8069074116b658a`.
- Speicher: `factorybox-current-storage-closure-plan-21381014af5446bdbf5c7eee2ad26181`.
- Weitere bestehende Drain-/CLI-Recovery-Assertions:
  `factorybox-current-release-handoff-ff320800515d412d836e5ac38d149dd9/remaining-current-coverage`.

Ein gesonderter historischer Aufräumversuch unter demselben Handoff schaltet
bei zwei ursprünglichen Proc-Testpaaren sowohl automatische Go-GC als auch
deren weiche Speichergrenze aus. Die harte Worker-Grenze bleibt 3 GiB.
Die qualifizierte Kernel-Beobachtung und die ursprüngliche unmittelbare
Unmount-Assertion bleiben erhalten. Der Versuch besteht mit 13 Haupttests und
65 Untertests. Es gibt keinen beobachteten EBUSY bei 231 erfassten Unmounts
und keine verlorenen Probe-Ereignisse. Das erklärt den historischen Fehler
weiterhin nicht; der geprüfte Nachweis liegt in
`gc-without-soft-limit/native-closure.json`.

Die ursprünglichen Anbieterpläne unter `current-external-acceptance/` bleiben
erhalten. Die tatsächliche Fortsetzung `remaining-after-storage-and-pid/` ist
inzwischen am ersten Anbieterfehler beendet. Der aktuelle Nachweis
`provider-first-failure/failure-closure.json` bindet die eine tatsächlich
zugestellte 429-Antwort, OpenCode 2.0.27, das erneut als kostenlos bestätigte
`glm-5.3-flash-free`, unveränderte Eingaben und das vollständige eigene
Aufräumen. Er bewahrt beide negativen Gesamtstände. Eine spätere Fortsetzung
braucht eine neue vorab festgelegte Erklärung und muss die gespeicherte
Wartefrist einhalten. Lokale Kontrollen und dieser Fehlernachweis sind keine
bestandene echte Anbieterabnahme.

Die erste weitere Speicherfolge besteht StoreFS, Basis-/Geräte-Recovery mit
allen Retained-Kontrollen und öffentliche Basisaufnahme. Sie stoppt vor dem
ersten Abschlussgrößen-Test an einem versehentlich ausgewählten, bereits
fehlgeschlagenen Diagnoseeingang. Mit dem vorhandenen vollständigen Eingang
stoppt der alte Test dann bei der richtigen früheren 2-MiB-Aufnahmegrenze,
bevor sein 4-MiB-Altbestand entsteht. Beide Fehler bleiben ausgewiesen.
Die getrennte Fixture `ledger-legacy-fixture/` erzeugt diesen Altbestand über
die bestehende explizite Controller-CLI auf dem eigenen Test-Store. Alle
ursprünglichen Prüfbedingungen für den anschließend normalen begrenzten
Worker, Daten, Fences, Unmounts und Aufräumen bleiben erhalten. Die Runtime
und ihre Aufnahmegrenze sind unverändert. Die neue, vollständig vorab
festgelegte Fortsetzung liegt in `acceptance-after-legacy-fixture-correction/`.
Die früheren eingereihten Folgen sind ohne eigene native Ausführung beendet.
Beide Abschlussgrenzen sind inzwischen bestanden. `storage-closure/closure.json`
prüft alle fünf tatsächlichen Ergebnisse, Quellen, Fixture-Änderung, rohe
Ausgaben und Traces sowie die Abwesenheit der eigenen Ressourcen erneut.

Auch der ursprüngliche PID-Gegenbeweis und seine Kontrolle bestehen mit der
tatsächlich ausgelieferten CRIU-Komponente `c0d11aeb...`. Der unveränderte
ursprüngliche Prozessabbild-Test und sämtliche Kernel-Assertions zeigen, dass
PID 2 erfolgreich belegt wird, während der absichtlich verzögerte echte Helfer
noch unter einer anderen PID existiert. Diese Komponentenprüfung verwendet
das ursprüngliche Test-Image; die vollständige aktuelle Paketprüfung bleibt
durch die getrennte 20er-Serie belegt. Der Nachweis liegt unter
`current-pid-seed-proof/closure.json`.

Die ausgewählte ursprüngliche 8-GiB-Sicherung bleibt vollständig erhalten.
Nur die inzwischen durch den erfolgreichen Rettungstest ersetzte Arbeitskopie
des Fehlversuchs `92e954...` wurde nach vollständiger Archivprüfung stillgelegt.
Alle 70.449 Einträge und 59.384 Datei-Hashes sowie 23 Diagnosedateien sind
gesichert. Quellen, Logs, ursprüngliche negative Ergebnisse und die getrennte
Original-Sicherung bleiben erhalten. Die überholten RAM-/Dateinutzdaten dieser
Arbeitskopie sind ausdrücklich nicht mehr rekonstruierbar. Netto wurden rund
819 MiB frei; Belege liegen unter `resolved-rescue-retention/` im Handoff.

## Messungen der vollständigen 20er-Serie

Je Beispiel sind zehn vollständige Läufe enthalten. Die folgende Tabelle zeigt
Median und beobachteten Bereich in Sekunden. Rootfs und Projektimages waren
vorab gebaut; jeder Lauf verwendet einen neuen eigenen Worker/32-GiB-Store.
Vorbereitung und erste nutzbare Anwendung sind getrennt gemessen.

| Messpunkt | Minimal: Median, Bereich | Factory: Median, Bereich |
| --- | ---: | ---: |
| Worker-Start | 0,91; 0,74–0,95 | 0,74; 0,55–0,78 |
| Rootfs-/Basisvorbereitung | 29,46; 15,66–38,00 | 24,92; 17,67–45,38 |
| Vorbereiteter Start bis nutzbare Anwendung | 9,10; 8,71–10,67 | 23,45; 19,94–26,08 |
| Neuer Worker mit Vorbereitung bis nutzbare Anwendung | 39,16; 26,02–49,36 | 51,02; 42,75–69,71 |
| Checkpoint der Quelle | 2,86; 2,60–3,26 | 14,41; 9,23–21,17 |
| Nutzbarer Fork eines vorhandenen Checkpoints | 3,18; 2,75–3,46 | 11,99; 7,44–15,38 |
| Neuer Checkpoint und nutzbarer Fork | 6,01; 5,41–6,72 | 24,34; 17,05–36,55 |
| Pause mit RAM-Freigabe | 2,77; 2,34–3,17 | 16,59; 10,06–18,42 |
| Nutzbares Resume | 2,17; 1,76–2,72 | 12,48; 7,98–14,69 |
| Neuer Checkpoint nach Resume und nutzbarer Fork | 5,18; 4,85–6,56 | 36,54; 21,81–38,86 |

In allen zwanzig Pausen sind anonymer RAM der pausierten Quelle und residente
Snapshot-Seiten null. Die vollständigen Worker enthalten weiterhin Dienste,
andere Test-/Anwendungsarbeit und Dateisystemcache. Ihre gemessenen Gesamtwerte
nach der Pause liegen bei 2,75–3,00 GB für Minimal und 2,16–2,70 GB für Factory.
Die unabhängigen Nachkommen sind Teil des vollständigen Tests; dies ist keine
Messung eines alleinstehenden leeren Workers.

Die getrennten IDE-Beobachtungen, Speicherzähler, Einzelwerte, genaue
Vorbereitung und Fehlerstände stehen in `final-performance/final-20.json`
unter dem Handoff. Die Messung läuft auf dem gemeinsamen Desktop-Host mit
unkontrolliertem Seitencache; zeitweise Archivarbeit auf dem Host ist im
Bericht vermerkt. Die Host-Vorbereitung der Testprogramme und die separate
IDE-Vorabprüfung sind eigene Werte. Diese Serie belegt keine p95, garantierte
Latenz, abgesicherte Beschleunigung oder Langzeitstabilität.
