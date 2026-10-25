# Gemessene Laufzeiten des aktuellen Kandidaten

Stand: 25. Oktober 2026. Die vorab festgelegte reguläre Serie besteht aus
zehn Minimal- und zehn Factory-Läufen. Alle 20 Versuche bestehen; kein
Fehlversuch wurde verworfen oder ersetzt. Gemessen wurden vollständige
Anwendungsabläufe mit erster nutzbarer Arbeit und Speicherfreigabe.
Die Produktionsfreigabe bleibt gemäß [Freigabestand](first-release-status.md)
offen.

Die Tabelle zeigt den Median in Sekunden, jeweils aus zehn Beobachtungen.
Minimal ist das kleinere Testsystem; Factory ist das größere
Entwicklungsbeispiel mit separater Theia-Instanz.

| Vorgang | Minimal | Factory |
| --- | ---: | ---: |
| Frischer Worker einschließlich Vorbereitung bis zur ersten nutzbaren Arbeit | 39,16 | 51,02 |
| Anwendungsstart bei bereits vorbereitetem Worker bis zur ersten nutzbaren Arbeit | 9,10 | 23,45 |
| Neue Sicherung und Klon bis zur ersten nutzbaren Arbeit im Klon | 6,01 | 24,34 |
| Pausieren einschließlich Sicherung und Speicherfreigabe | 2,77 | 16,59 |
| Fortsetzen einer Pause bis zur ersten nutzbaren Arbeit | 2,17 | 12,48 |

Die reine Vorbereitung dauert im Median 29,46 beziehungsweise 24,92 Sekunden.
Sie umfasst das Entpacken des eingefrorenen Root-Dateisystems, Testeingaben,
Import und vollständige Prüfung der unveränderlichen Basis sowie die
OCI-Vorbereitung. Die Worker- und Dienststarts werden zusätzlich erfasst.
Die vorbereiteten Startzeiten enthalten diese vorherige Vorbereitung nicht.
Mediane einzelner Phasen dürfen nicht zum Median des Gesamtvorgangs addiert
werden. Die separate Vorbereitung des Testprogramms auf dem Host gehört
nicht zu diesen Anwendungszeiten.

Beim Fortsetzen lagen die beobachteten Werte zwischen 1,76 und 2,72 Sekunden
für Minimal sowie 7,98 und 14,69 Sekunden für Factory. Beim Pausieren lagen
sie zwischen 2,34 und 3,17 beziehungsweise 10,06 und 18,42 Sekunden.

Nach dem Pausieren sind in allen 20 Läufen sowohl der anonyme RAM der
pausierten Anwendung als auch die residenten Sicherungsdaten null. Der
gesamte Worker hält weiterhin Speicher für Dienste, unabhängige weiterlaufende
Prozesse und Dateisystemcache. Seine gemessenen Zähler nach der Pause liegen
bei 2,75 bis 3,00 GB für Minimal und 2,16 bis 2,70 GB für Factory.

Die Worker-Grenze beträgt 3 CPU und 3 GiB; die separate Theia-Instanz hat
1 CPU und 768 MiB. Die Läufe verwenden vorab gebaute, festgelegte Images auf
dem gemeinsam genutzten Desktop-Host. Es gab keine kontrollierte
Cache-Leerung; zeitweise lief geschlossene Belegverwaltung auf dem Host.
Die Zahlen beschreiben diese Beobachtungen und belegen weder p95, garantierte
Latenzen noch eine Beschleunigung gegenüber einem kontrollierten Vergleich.
Die Serie verwendet ein lokales deterministisches Modell. Sie misst keine
Antwortzeiten eines externen Modellanbieters und ersetzt dessen offene Abnahme
nicht. Worker-Verlust-Recovery ist durch getrennte Tests belegt und wird
nicht mit der hier angegebenen normalen Fortsetzungszeit gleichgesetzt.

Der vollständige Nachweis liegt lokal unter
`artifacts/factorybox-current-release-handoff-ff320800515d412d836e5ac38d149dd9/final-performance/final-20.json`,
SHA-256 `ffad7d36f78151550e634ee7dc142a7cbd2a160bc0a0cb5ded984149b15c4d3b`.
Er bindet jeden Versuch, die Einzelzeiten, Speicherbeobachtungen und die
Serienerklärung
`factorybox-first-release-ordinary-series-97f5b8e9af9846f7b00f34e793dbf6d3`.
Große Eingaben und Rohbelege bleiben gemäß Aufbewahrungsregeln lokal in
`artifacts/` und sind vom Git-Push ausgeschlossen.
