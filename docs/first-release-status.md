# Stand der ersten lokalen Freigabe

Stand: 24. Oktober 2026. Die Freigabe ist offen. Verbindlicher Umfang bleibt
[GOAL.md](../GOAL.md), einschließlich aller dort genannten Grenzen.
Der 24-Stunden-Dauerlauf und die 100er-Serie gehören zur späteren Etappe.

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

Die Serien `93d21ac9...` und `a3d09ea4...` hielten nach fünf beziehungsweise
acht weiteren Erfolgen vor der nächsten Gruppe an der Artefaktreserve an.
Diese Gruppen hatten noch nicht begonnen. Beide negativen Gesamtstände
bleiben erhalten. Die letzte Erklärung
`factorybox-current-native-final-b19c38f289cd492084650d66423f79a0`
übernimmt die 13 geprüften Erfolge, führt nur die drei offenen Gruppen aus
und besteht vollständig. Zusammen mit dem vorherigen vollständigen Pause-Test
sind damit alle 17 Hauptgruppen belegt. Die reguläre 20er-Serie ist ungestartet.

| Anforderung | Belegter Stand des aktuellen Kandidaten | Noch erforderlich |
| --- | --- | --- |
| Paket reproduzieren | 6016 und 829f sind identisch. | Endgültiges Abnahmeprofil mit allen Ergebnissen verbinden. |
| Native Laufzeit, Speicher und Stilllegung | Alle 17 Hauptgruppen, elf weitere gezielte Timer-/Restore-Hold-/Metadatenfälle, die vollständige Stilllegungssuite mit acht Haupttests und der Kapazitätsfall bestehen; CRIU-Kernprüfung ist bytegleich gebunden. | Die beiden unterbrochenen nativen Epochenfälle auf dem aktuellen Paket abschließen und im Endprofil verbinden. |
| Vollständige Integration | Frühere Docker- und Komponentenbelege bleiben erhalten; unveränderte Komponenten können exakt gebunden werden. | Alle 15 benannten Fälle auf dem aktuellen Stand nachweisen: fünf Retained-, sechs Docker-Abbruch-, zwei volle Verbraucher- und zwei Komponentenfälle. |
| Anwendung bei vollem Speicher wiederherstellen | Der vollständige harte Factory-Verlust besteht auf 6016. | Geplanten Factory-Fall, beide Minimal-Fälle und beide unterbrochenen nativen Epochenfälle auf demselben Paket abschließen. |
| Ursprüngliche 8-GiB-Sicherung weiterbenutzen | Alle 322 RAM-/Metadatendateien und neun Dateisystembäume sind geprüft und unverändert. | Tatsächlich wiederherstellen und nützliche Arbeit prüfen. |
| Alten Busy-Mount-Fehler erklären | Spätere Wiederholungen und gezielte Ursachen-Kontrollen sind erhalten. | Ursprünglichen Fehler Test oder Laufzeit zuordnen; nötige Korrektur belegen. |
| Echten externen Agenten fortsetzen | Ursprünglicher Anbieterfehler bleibt erhalten. Die lokale Modell-Fixture ist kein Anbieterbeleg. | Echte Fortsetzung, Abbruch und unklare Werkzeugwirkungen nachweisen. Keine Wiederholung vor `2026-10-24T23:59:59.953093Z`. |
| Update, Drain und Rollback prüfen | Geprüfte Eingaben des Drain-Unterbrechers und Betriebsanleitung liegen vor. | Vollständige Betriebsfälle auf 6016, Kompatibilität oder kontrolliertes Leeren vor einem Wechsel. Kein bekannt fehlerhaftes Paket als qualifizierte Rückfallversion. |
| Reguläre Wiederholungsserie | Alle früheren Serien haben null Läufe. Der bestandene Fehlerfall zählt nicht zur Serie. | Neue Erklärung für das endgültige Paket vor dem ersten Lauf festlegen, dann zehn vollständige Läufe je Beispiel ohne verworfene Fehler durchführen. |

Die [Fortschrittsbelege](production-progress.md) enthalten die einzelnen
Artefakte, Fehler und Ersetzungen. Die
[Betriebsanleitung](first-release-operations.md) bleibt bis zur vollständigen
Abnahme eine Anleitung für den geprüften Entwicklungsstand.

Die bisher jüngste, an ein älteres Paket gebundene Serienerklärung liegt unter
`artifacts/factorybox-first-release-ordinary-series-c5663e4172ef495485621d537afbbef9/plan.json`,
SHA-256 `ec9cbaa19c68ce0e22b6587fa827de5fe49bf3131fd5de2422c5cd72c559608d`.
Sie und alle Vorgänger bleiben unverändert mit null ausgeführten Läufen.
Für 6016 ist eine neue Erklärung erforderlich. Sie muss alle 20 Befehle,
ursprünglichen Prüfungen und die volle Speicherreserve erhalten.
