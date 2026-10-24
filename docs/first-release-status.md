# Stand der ersten lokalen Freigabe

Stand: 24. Oktober 2026. Die Freigabe ist offen. Verbindlicher Umfang bleibt
[GOAL.md](../GOAL.md), einschließlich aller dort genannten Grenzen.
Der 24-Stunden-Dauerlauf und die 100er-Serie gehören zur späteren Etappe.

Ausgewählt ist Worker `130870231710425a8ee16a71c34a611e`, unabhängig identisch
nachgebaut als `361170cd105a4c9a84f44bc5029430f3`. Sein Paket hat SHA-256
`0af860a4edf1c12a7bc135d6c5917b33d3f172ac64e3ad555e8039ffb9b454b7`.
Ein erfolgreicher Test eines älteren Laufzeitkerns ersetzt dessen Abnahme nicht.

Der zuletzt untersuchte konkrete Freigabeblocker war ein fehlgeschlagenes erneutes Resume
im vollständigen Factory-Fall mit hartem Worker-Abbruch und Speicherdruck.
Die erste Wiederherstellung funktioniert. Die weitere Pause speichert eine
unvollständige Prozessliste: CRIU weist einen beendeten Prozess mit anderem
PID-Namensraum als sein Elternprozess ab, meldet die Sicherung aber trotzdem
als erfolgreich. Beim späteren Resume stürzt CRIU wegen eines dadurch fehlenden
Prozesses ab; die ausbleibende interne Restore-Bestätigung ist die Folge.
Der vollständige Fehlerzustand `ad6530e1a56440fd9dcef4545b53f08b` ist gesichert.
Ein echter kleiner Linux-Test reproduziert dieselbe falsche Erfolgsmeldung in
0,115 Sekunden. Der neue CRIU-Kandidat `a93016e0ec514e5b9ea84f7d142b875f`
korrigiert die Fehlerweitergabe und stellt den beendeten Prozess mit seinem
ursprünglichen Elternprozess, beiden PID-Ebenen und Exit-Status wieder her.
Der echte Linux-Gegenbeweis besteht nun einschließlich eines anschließenden
normalen Forks; sieben Fehlerweitergabe- und 18 unveränderte CRIU-Kernfälle
bestehen ebenfalls. Ein unabhängiger CRIU-Neubau ist binäridentisch.
Das neue Worker-Paket `245c0cf79c3a40b59b5bb45d482b242c` ist gebaut, aber
noch nicht ausgewählt oder vollständig abgenommen. Der ursprüngliche vollständige
Factory-Fall besteht damit unter `98bb5cea91594ffa85d1d91305eb8295`
vollständig: echter ENOSPC, erhaltene alte Pause, abgebrochener Teilimport,
Loop-Wechsel, unabhängige neue Pause, Stilllegung der alten Epoche,
nützliches zweites Resume und anschließender Checkpoint/Fork. Aufräumen und
unveränderte globale Einstellungen sind bestätigt. Die übrige Abnahme des
neuen Pakets bleibt offen. Die 20er-Serie hat nicht begonnen; die
Produktionsqualifikation bleibt false.

| Anforderung | Belegter Stand auf dem ausgewählten Paket | Noch erforderlich |
| --- | --- | --- |
| Identisches Paket erneut bauen | Beide Builds von Worker 1308 stimmen überein. Der neue Worker 245c ist gebaut; seine CRIU-Komponente ist unabhängig identisch nachgebaut. | Neuen Worker unabhängig nachbauen und Abnahmeprofil mit den endgültigen Ergebnissen verbinden. |
| Laufzeit und Speicher absichern | Auf Worker 1308 bestehen alle 17 Go-/Laufzeit-/Speichergruppen und die Retirement-Suite mit acht Haupttests. Die neue CRIU-Komponente besteht alle 18 Kernfälle. | Betroffene native Gruppen und vollständige Anwendungstests auf dem endgültigen neuen Paket. |
| Letzten Timer-Recoveryfehler beheben | Beide ursprünglichen Docker-Abbruchfälle bestehen mit nützlicher Anwendung, weiteren Sicherungen und unabhängigen Nachkommen. Beide Verweigerungsfälle ohne Berechtigung bestehen ebenfalls. | Übrige Integration auf demselben Paket abschließen. |
| Vollständige Integration | Alle sechs Docker-Abbruchfälle bestanden; zwei unveränderte Upstream-Komponentenfälle genau gebunden. | Fünf Retained-Fälle und zwei vollständige Verbraucherfälle, insgesamt alle 15 benannten Integrationsfälle. |
| Anwendung bei vollem Speicher wiederherstellen | Factory mit geplantem Worker-Ersatz besteht auf Worker 1308. Der harte Verlust einschließlich erneutem Resume besteht nach erklärtem und korrigiertem Capture-Fehler auf Worker 245c; der ursprüngliche vollständige Fehlerzustand bleibt erhalten. | Geplanten Factory-Fall auf dem neuen Paket, beide Minimal- und beide unterbrochenen Epochenfälle abschließen. |
| Ursprüngliche 8-GiB-Sicherung weiterbenutzen | Alle 322 RAM-/Metadatendateien und neun Dateisystembäume sind geprüft und unverändert. | Tatsächliche Wiederherstellung mit anschließender nützlicher Arbeit. Eine reine Inhaltsprüfung genügt nicht. |
| Alten Busy-Mount-Fehler erklären | Spätere Wiederholungen und gezielte Ursachen-Kontrollen sind erhalten. | Ursprünglichen Fehler Test oder Laufzeit zuordnen; nötige Korrektur belegen. |
| Echten externen Agenten fortsetzen | Ursprünglicher Anbieterfehler bleibt als Fehler erhalten. | Tatsächliche Fortsetzung, Abbruch und Umgang mit unklaren Werkzeugwirkungen nachweisen. Keine Wiederholung vor dem protokollierten Kontingent-Ende `2026-10-24T23:59:59.953093Z`. |
| Update, Drain und Rückkehr prüfen | Die Eingaben des gezielten Drain-Unterbrechers sind geprüft. | Vollständige native Betriebsfälle auf dem ausgewählten Paket, dokumentierte Kompatibilität oder kontrolliertes Leeren vor einem Wechsel. |
| Reguläre Wiederholungsserie | Genau zehn vollständige Läufe je Beispiel vorab deklariert; Eingaben geprüft, kein Lauf begonnen. | Alle 20 durchführen, jeden Fehler erhalten, Messwerte und Grenzen berichten. |

Die [Fortschrittsbelege](production-progress.md) enthalten die einzelnen
Artefakte, Fehler und Ersetzungen. Die
[Betriebsanleitung](first-release-operations.md) bleibt bis zur vollständigen
Abnahme eine Anleitung für den geprüften Entwicklungsstand.

Die jüngste Serienerklärung liegt unter
`artifacts/factorybox-first-release-ordinary-series-c5663e4172ef495485621d537afbbef9/plan.json`,
SHA-256 `ec9cbaa19c68ce0e22b6587fa827de5fe49bf3131fd5de2422c5cd72c559608d`.
Alle Vorgängererklärungen bleiben unverändert. Die neue Erklärung erhält alle
20 Befehle und ursprünglichen Prüfungen sowie die volle Speicherreserve.
