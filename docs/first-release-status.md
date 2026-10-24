# Stand der ersten lokalen Freigabe

Stand: 25. Oktober 2026. Die Freigabe ist offen. Verbindlicher Umfang bleibt
[GOAL.md](../GOAL.md), einschließlich aller dort genannten Grenzen.
Der 24-Stunden-Dauerlauf und die 100er-Serie gehören zur späteren Etappe.

Die reguläre Serie ist vollständig bestanden und unabhängig geprüft: zehn
Minimal- und zehn Factory-Läufe, ohne Fehlschlag, Wiederholung oder verworfenen
Versuch. Der ursprüngliche Ablauf ist beendet. Das vollständige Ergebnis steht
in `artifacts/factorybox-first-release-ordinary-series-97f5b8e9af9846f7b00f34e793dbf6d3/result.json`.
Die fünf Betriebsfälle sind anschließend vollständig bestanden und unabhängig
geprüft: Startfehler, Speicheridentität, alle acht Stop-Schutzfälle, der gelieferte
Compose-Ablauf und tatsächlicher Update/Rückfall. Auch alle fünf weiteren
Speicherfälle sind bestanden und unabhängig geprüft. Jetzt sind auch alle vier
zusätzlichen Drain-Fälle und beide ursprünglichen CLI-Worker-Verlustarten
bestanden und unabhängig geprüft. Die Gateway-/Anbieterfälle, die historische
Busy-Mount-Zuordnung und das endgültige Freigabeprofil bleiben offen.
Ein eingereihter Test ist kein Pass.

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
Das ist keine Latenzverteilung oder Garantie. Die ursprünglichen vollständigen
Fehlerbelege `ad6530e1...` und `aaf8fa3...` bleiben erhalten.

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
sind damit alle 17 Hauptgruppen belegt. Die reguläre 20er-Serie läuft inzwischen
unter der unten benannten neuen Erklärung.

| Anforderung | Belegter Stand des aktuellen Kandidaten | Noch erforderlich |
| --- | --- | --- |
| Paket reproduzieren | 6016 und 829f sind identisch. | Endgültiges Abnahmeprofil mit allen Ergebnissen verbinden. |
| Native Laufzeit, Speicher und Stilllegung | Alle 17 Hauptgruppen, elf weitere gezielte Timer-/Restore-Hold-/Metadatenfälle, die vollständige Stilllegungssuite, Kapazität und beide unterbrochenen nativen Epochenfälle bestehen; CRIU-Kernprüfung ist bytegleich gebunden. Alle fünf zusätzlichen Speicherfälle einschließlich beider Abschlussgrößen-Grenzfälle mit erklärter Altbestand-Fixture sind bestanden und unabhängig geprüft. | Die Nachweise im Endprofil verbinden. |
| Vollständige Integration | Alle 15 benannten Fälle sind unabhängig geprüft: fünf Retained-, sechs Docker-Abbruch-, zwei vollständige Anwendungs- und zwei unveränderte Komponentenfälle. Die erwartete Ablehnung beschädigten RAMs und ursprünglichen Upstream-Skips bleiben ausgewiesen. | Nachweise im endgültigen Abnahmeprofil verbinden. |
| Anwendung bei vollem Speicher wiederherstellen | Alle vier vollständigen Factory-/Minimal-Fälle bei geplantem und hartem Worker-Verlust bestehen auf 6016, einschließlich Platzmangel und Epochenstilllegung. | Nachweise im endgültigen Abnahmeprofil verbinden. |
| Ursprüngliche 8-GiB-Sicherung weiterbenutzen | Bestanden in `factorybox-enospc-original-rescue-e66b1b1a3bdb4cf39d49f6c67d6eb13d`: unverändertes schreibgeschütztes Original auf Loop0, unabhängiger Import und echte Wiederherstellung mit dem ursprünglichen CLI-/CRIU-Rezept. Vollständiger ursprünglicher Anwendungszustand und gespeicherter Agentenabbruch stimmen überein; eine neue Anwendungsänderung besteht ohne Werkzeug-Replay. Originalabbild unverändert, Aufräumen vollständig, insgesamt 93,33 Sekunden. Die tatsächlichen Dienstpfade des frischen Workers mussten dem gespeicherten Rezept entsprechen. | Nachweis ins Endprofil aufnehmen. Dieser forensische Weg qualifiziert weder das alte Paket als Rückfallversion noch einen Rezeptwechsel für alte Sicherungen. |
| Alten Busy-Mount-Fehler erklären | Spätere Wiederholungen und gezielte Ursachen-Kontrollen sind erhalten. | Ursprünglichen Fehler Test oder Laufzeit zuordnen; nötige Korrektur belegen. |
| Echten externen Agenten fortsetzen | Ursprünglicher Anbieterfehler bleibt erhalten. Die lokale Modell-Fixture ist kein Anbieterbeleg. Die acht bestehenden echten Anbieterfälle sind für 6016 festgelegt und hinter den vorherigen Abnahmen eingereiht. | Die beiden nativen Gateway-Kontrollen und alle acht vollständigen Anbieterfälle unabhängig abnehmen: normaler Ablauf, einmal verlorener abgeschlossener Modellaufruf, beide Worker-Verlustarten sowie Abbruch und unklare Werkzeugwirkung jeweils bei geplantem/hartem Verlust. Keine Inferenz vor `2026-10-24T23:59:59.953093Z`. |
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

Die Anbieterfolge liegt unter demselben Handoff in
`current-external-acceptance/`. Sie wartet auf den vollständigen Abschluss der
vorhergehenden Prüffolge und wahrt zusätzlich die gespeicherte Kontingentsperre.
Jeder ursprüngliche Verbraucher prüft das ausdrücklich kostenlose Modell
erneut. Ein fehlgeschlagener Lauf hält die Folge an; es gibt keinen Wechsel
von Modell, Identität oder Kostenklasse. Die Einreihung ist kein Anbieter-Pass.

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
