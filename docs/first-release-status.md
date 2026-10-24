# Stand der ersten lokalen Freigabe

Stand: 24. Oktober 2026. Die Freigabe ist offen. Verbindlicher Umfang bleibt
[GOAL.md](../GOAL.md), einschließlich aller dort genannten Grenzen.
Der 24-Stunden-Dauerlauf und die 100er-Serie gehören zur späteren Etappe.

Die reguläre Serie hat am 24. Oktober um 21:25 UTC neun vollständige Läufe
bestanden; der zehnte läuft. Das laufende Ergebnis steht in
`artifacts/factorybox-first-release-ordinary-series-97f5b8e9af9846f7b00f34e793dbf6d3/progress.json`.
Die Betriebs-, Speicher- und Drain-Prüfungen sind mit festen Eingaben dahinter
eingereiht. Sie starten erst nach erfolgreichem Abschluss der vorhergehenden
Prüffolge und erhalten eigene Ergebnisse. Ein eingereihter Test ist kein Pass.

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
| Native Laufzeit, Speicher und Stilllegung | Alle 17 Hauptgruppen, elf weitere gezielte Timer-/Restore-Hold-/Metadatenfälle, die vollständige Stilllegungssuite, Kapazität und beide unterbrochenen nativen Epochenfälle bestehen; CRIU-Kernprüfung ist bytegleich gebunden. | Fünf weitere bestehende Speicher-Prüfaufrufe auf dem endgültigen Paket ausführen: StoreFS, Basis-/Geräte-Recovery mit Retained-Kontrollen, öffentliche Basisaufnahme und beide Abschlussgrößen-Grenzfälle. Die Nachweise im Endprofil verbinden. |
| Vollständige Integration | Alle 15 benannten Fälle sind unabhängig geprüft: fünf Retained-, sechs Docker-Abbruch-, zwei vollständige Anwendungs- und zwei unveränderte Komponentenfälle. Die erwartete Ablehnung beschädigten RAMs und ursprünglichen Upstream-Skips bleiben ausgewiesen. | Nachweise im endgültigen Abnahmeprofil verbinden. |
| Anwendung bei vollem Speicher wiederherstellen | Alle vier vollständigen Factory-/Minimal-Fälle bei geplantem und hartem Worker-Verlust bestehen auf 6016, einschließlich Platzmangel und Epochenstilllegung. | Nachweise im endgültigen Abnahmeprofil verbinden. |
| Ursprüngliche 8-GiB-Sicherung weiterbenutzen | Bestanden in `factorybox-enospc-original-rescue-e66b1b1a3bdb4cf39d49f6c67d6eb13d`: unverändertes schreibgeschütztes Original auf Loop0, unabhängiger Import und echte Wiederherstellung mit dem ursprünglichen CLI-/CRIU-Rezept. Vollständiger ursprünglicher Anwendungszustand und gespeicherter Agentenabbruch stimmen überein; eine neue Anwendungsänderung besteht ohne Werkzeug-Replay. Originalabbild unverändert, Aufräumen vollständig, insgesamt 93,33 Sekunden. Die tatsächlichen Dienstpfade des frischen Workers mussten dem gespeicherten Rezept entsprechen. | Nachweis ins Endprofil aufnehmen. Dieser forensische Weg qualifiziert weder das alte Paket als Rückfallversion noch einen Rezeptwechsel für alte Sicherungen. |
| Alten Busy-Mount-Fehler erklären | Spätere Wiederholungen und gezielte Ursachen-Kontrollen sind erhalten. | Ursprünglichen Fehler Test oder Laufzeit zuordnen; nötige Korrektur belegen. |
| Echten externen Agenten fortsetzen | Ursprünglicher Anbieterfehler bleibt erhalten. Die lokale Modell-Fixture ist kein Anbieterbeleg. | Echte Fortsetzung, Abbruch und unklare Werkzeugwirkungen nachweisen. Keine Wiederholung vor `2026-10-24T23:59:59.953093Z`. |
| Update, Drain und Rollback prüfen | Betriebsanleitung und festgelegte Prüffolgen liegen vor. Die vier vorhandenen Drain-Abbruchfälle und der ursprüngliche separate CLI-Worker-Recovery-Test haben bisher nur ältere Paketnachweise. | Startfehler, Speicheridentität, alle acht Stop-Schutzfälle, gelieferten Compose-Ablauf und tatsächlichen Update-/Rückfallversuch auf 6016 abschließen; außerdem beide Verbraucher mit/ohne erzwungenen Loop-Wechsel beim Drain-Abbruch sowie beide ursprünglichen CLI-Worker-Verlustarten prüfen. Kompatibilität oder kontrolliertes Leeren vor einem Rezeptwechsel; kein bekannt fehlerhaftes Paket als qualifizierte Rückfallversion. |
| Reguläre Wiederholungsserie | Neue vollständige Erklärung für 6016/829f ist vorab eingefroren und geprüft. Die Serie `factorybox-first-release-ordinary-series-97f5b8e9af9846f7b00f34e793dbf6d3` ist gestartet. Frühere Serien behalten null Läufe; Fehlerfälle zählen nicht rückwirkend mit. | Alle 20 festgelegten vollständigen Läufe ausführen und unabhängig prüfen. Kein Fehlschlag wird ersetzt oder verworfen. |

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
Unmount-Assertion bleiben erhalten. Der Versuch ist noch nicht ausgeführt;
auch ein Erfolg würde die historische Fehlerursache nicht erklären.
