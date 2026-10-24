# Stand der ersten lokalen Freigabe

Stand: 24. Oktober 2026. Die Freigabe ist offen. Verbindlicher Umfang bleibt
[GOAL.md](../GOAL.md), einschließlich aller dort genannten Grenzen.
Der 24-Stunden-Dauerlauf und die 100er-Serie gehören zur späteren Etappe.

Ausgewählt ist Worker `130870231710425a8ee16a71c34a611e`, unabhängig identisch
nachgebaut als `361170cd105a4c9a84f44bc5029430f3`. Sein Paket hat SHA-256
`0af860a4edf1c12a7bc135d6c5917b33d3f172ac64e3ad555e8039ffb9b454b7`.
Ein erfolgreicher Test eines älteren Laufzeitkerns ersetzt dessen Abnahme nicht.

| Anforderung | Belegter Stand auf dem ausgewählten Paket | Noch erforderlich |
| --- | --- | --- |
| Identisches Paket erneut bauen | Beide unabhängigen Builds stimmen überein. | Abnahmeprofil mit den endgültigen Ergebnissen verbinden. |
| Laufzeit und Speicher absichern | Alle 17 Go-/Laufzeit-/Speichergruppen neu bestanden; unveränderte CRIU-Gruppe über genaue Komponentenbindung geprüft. Die vollständige Retirement-Suite besteht mit allen acht Haupttests. | Vollständige Anwendungstests und Betriebsfälle unten. |
| Letzten Timer-Recoveryfehler beheben | Beide ursprünglichen Docker-Abbruchfälle bestehen mit nützlicher Anwendung, weiteren Sicherungen und unabhängigen Nachkommen. Beide Verweigerungsfälle ohne Berechtigung bestehen ebenfalls. | Übrige Integration auf demselben Paket abschließen. |
| Vollständige Integration | Alle sechs Docker-Abbruchfälle bestanden; zwei unveränderte Upstream-Komponentenfälle genau gebunden. | Fünf Retained-Fälle und zwei vollständige Verbraucherfälle, insgesamt alle 15 benannten Integrationsfälle. |
| Anwendung bei vollem Speicher wiederherstellen | Vier vollständige ältere Anwendungsfälle sind erhalten. | Alle vier auf dem ausgewählten Paket neu prüfen, außerdem beide unterbrochenen Epochenfälle. |
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
