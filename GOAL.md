# Factorybox: lokale Produktionsfreigabe

Am 2026-10-22 nach der Grilling-Runde bestätigt und als aktives Goal gestartet.
Ziel ist eine für diesen Linux-AMD64-Rechner und die vorhandenen Beispiele
freigegebene Version. Eine Freigabe erfolgt erst nach bestandener Abnahme;
der bisherige Stand bleibt unverändert als Vergleich erhalten.

Der Ausgangsstand ist Commit
`521e05799853c8546c9db5de31d178c548c49573` und das
[bisherige Referenzprofil](upstream/amd64-pilot-profile.json). Das vorangegangene
abgeschlossene Goal, der Quellmanifest und dessen Eingaben liegen unter
`artifacts/factorybox-production-baseline-0c99cab81e224d1db537b81d12a2e2f9/`.
Der [Abnahmebericht](docs/pilot-validation.md) und die Referenzartefakte bleiben
historische Nachweise. Produktionsqualifikation ist zu Beginn dieses Goals false.

## Einsatzgrenze

- Vorhandener Linux-AMD64-Rechner, geprüfter Kernel, 4096-Byte-Seiten,
  cgroup v2 und konkret geprüfter lokaler Speicherpfad.
- Getrennte Worker je Kunde oder Vertrauensbereich. Gemeinsamer Host-Kernel,
  privilegierter äußerer Verwaltungs-Worker, keine behauptete VM-Isolation.
- Worker mit höchstens 3 CPU/3 GiB, höchstens zwei lebende Sandboxes;
  separate Theia mit 1 CPU/768 MiB und 420 Sekunden je nativem Lifecycle-Lauf.
- Wiederherstellung einer vollständig bestätigten Pause nach geplantem oder
  abruptem Worker-Ersatz auf demselben Host und Boot mit intaktem eigenen Store.
- Kubernetes, andere Plattformen, Host-Neustart-RAM-Recovery und Cross-Node-
  Migration sind keine Anforderungen dieser Etappe.
- Keine Änderungen an Factory-Repositories oder historischem Experiment.
  Kein KVM, neuer Kernel, neue VM, Host-Runtime-Installation oder globale sysctls.

## 1. Bestehenden vollständigen Lifecycle erhalten

Das minimale Dev-Container-Beispiel und das vollständige Factory-Beispiel mit
privatem Docker, Compose, Dev Containers und separater Theia müssen mit den
tatsächlich ausgelieferten Bytes weiter bestehen:

- Start, Status, vollständiger RAM-/Dateisystem-Checkpoint und unabhängiger Fork.
- Unabhängige RAM-/Dateiänderungen, Checkpoint/Fork eines Forks, brauchbarer
  Nachkomme nach Löschung seiner ursprünglichen Quelle.
- Pause mit tatsächlicher Freigabe des Workload-RAMs und der residenten
  Snapshot-RAM-Daten; nützliches Resume, erneuter Checkpoint/Fork danach.
- Bestätigte Pause nach vollständigem Worker-Verlust unabhängig importieren,
  fortsetzen, erneut checkpointen/forken und exakt wiederholen können.
- Unvollständige oder fremde Operationen ablehnen; ausschließlich exakt
  besessene Prozesse, Mounts, cgroups und Speicherobjekte stilllegen.
- IDE, Agent und Dev Containers bleiben optionale Verbraucher.

## 2. Echte externe Agentenfortsetzung

Das gepinnte OpenCode mit einem nachweislich kostenlosen, werkzeugfähigen
externen Modell verbinden. Modell-ID, Schnittstelle, Kostenstatus, Zeitpunkt,
Verfügbarkeit und genaue Testeingaben dokumentieren. Kein stiller Wechsel zu
kostenpflichtigen Modellen oder anderer OpenCode-Version. Zugangsdaten nicht
in Git, Logs oder öffentliche Artefakte schreiben. Nur synthetische Testdaten
an den Anbieter senden.

Der reale Agent muss ein tatsächliches Werkzeug ausführen, dessen private
RAM-/Dateidaten und Dateiwatches im vollständigen Lifecycle erhalten bleiben.
Pause/Resume, Trennung/Wiederverbindung, vollständig bestätigte Pause nach
Worker-Ersatz und unabhängiger anschließender Fork sind getrennt nachzuweisen.
Nach Fortsetzung muss der Agent das tatsächliche Werkzeugergebnis und dessen
Exitstatus verarbeiten. Keine unbeabsichtigte Wiederholung eines unklaren
externen Werkzeugergebnisses; keine pauschale Exactly-once-Garantie.

Der deterministische lokale Modellserver bleibt für reproduzierbare negative
Prüfungen. Ein Anbieterfehler, Rate-Limit oder fehlender Zugang ist kein
bestandener Test. Unabhängige Laufzeitarbeit kann dabei fortgesetzt werden.

## 3. Frischen Factory-Start verkürzen

Den frischen Start priorisieren. Die bereits bewiesene verifizierte Basis
konsequent für weitere unabhängige frische Umgebungen verwenden. Den bisherigen
ungefähr 33 Sekunden langen Anwendungsstart in Docker, Image-Bereitstellung,
Connector, Dev-Container-Einrichtung und erste nützliche Antwort zerlegen.
Nur konkret gemessene Engpässe ändern.

Vollständige kalte Vorbereitung bleibt im Bericht sichtbar. Wiederverwendung
einer schon vorbereiteten Basis ist keine Beschleunigung ihrer erstmaligen
Vorbereitung. Eine echte frische Umgebung erbt keinen Agenten-RAM-Zustand.
Bestehende Verbraucherdateien bleiben als Vergleich erhalten; optimierte
Varianten und ihre exakten Änderungen werden getrennt dokumentiert.

Vergleichbare Beobachtungen gegen den eingefrorenen Ausgangsstand müssen den
ausgewählten Gewinn nachweisen. Vorbereitung, erste nützliche Anwendung,
separate IDE-Prüfungen, Checkpoint/Fork, Pause/Resume, Speicher, I/O und Fehler
berichten. Keine Regression der bisherigen Zustands-/Integritätsprüfungen.
Für Start-/Fork-/Resume-Zeiten die vorherige Schranke von höchstens dem größeren
Wert aus 20 Prozent und 250 ms als konservativen Vergleich übernehmen;
Abweichungen ausdrücklich untersuchen, nicht durch veränderte Eingaben verdecken.

Keine p95-Garantie aus wenigen Beobachtungen. SHM, Lazy-/Delta-Snapshots und
Lesehinweis-Experimente bleiben ohne eigene vollständige Qualifikation ungewählt.

## 4. Speicher für Dauerbetrieb

Gemessene Store-Kapazität, begrenzte Aufnahme neuer Schreiboperationen und
Fail-closed-Verhalten bei Platzmangel liefern. Alter Checkpoint, lebende
Sandbox und belegte Wiederherstellungsdaten bleiben bei einer abgelehnten oder
unterbrochenen Operation erhalten.

Eine ausdrücklich angeforderte Stilllegung alter Checkpoints und Worker-
Epochen muss genaue Besitz- und Abhängigkeitsprüfungen haben. Keine lebende
Quelle, kein Nachkomme und kein ausgewählter Recovery-Eingang darf dadurch
benötigte Daten verlieren. Unvollständige Operationen und fremde Dateien nicht
blind aufräumen. Wiederholte Stilllegung muss ihren exakten eigenen Abschluss
erkennen. Reine Sandbox-Löschung und Datenstilllegung getrennt dokumentieren.

## 5. Wiederholung, Fehler und Starttimeout

Den erhaltenen Sysbox-fs-Starttimeout auf diesem Host gezielt untersuchen.
Ursache, konkrete Reproduktion und eventuelle Korrektur belegen. Erfolgreiche
spätere Starts erklären keinen früheren Timeout. Ohne belegte Ursache keine
unverifizierte SHM-Änderung auswählen.

Als anfänglichen Freigabeumfang mindestens 100 vollständige Lifecycle-Läufe
je Beispiel und einen begrenzten 24-Stunden-Dauerlauf anstreben. Positive und
negative Fälle, Trennung, Worker-Verlust, Speicher-/Platzdruck und exakte
Wiederholung einbeziehen. Keine Daten- oder Besitzverletzung und keine
untersuchten Fehler stillschweigend verwerfen. Ausfälle vollständig ausweisen.
Der Umfang ist ein lokaler Belastungsnachweis, keine behauptete Langzeit-SLA.

Die Artefaktgrenzen gelten auch im Dauerlauf. Referenz, eingefrorene Eingaben,
aktive Läufe und ausgewählte unaufgelöste Fehler bewahren. Neue Builds/native
Läufe nur nach Kapazitätsprüfung; abgeschlossene Belege ausschließlich mit
geprüften Retentions-/Kompressionsbelegen verkleinern.

## 6. Betriebsfähiges reproduzierbares Paket

Ein genau identifiziertes Worker-Paket/Image mit notwendigen Bibliotheken,
Komponenten-/Abhängigkeitsinventar, Patchfolge und Lizenzhinweisen bauen.
Unnötige Build-/Testwerkzeuge aus dem auszuliefernden Image entfernen und die
resultierenden Bytes neu abnehmen. Unabhängige Wiederholungsbuilds müssen
Paket- und Binäridentität belegen.

Bereitschaft, Laufzeitmessungen, Ressourcen, Fehler und Kapazität beobachtbar
machen. Anleitungen für Aufnahme neuer Arbeit, Drain, bestätigte Pause,
Fencing, Wiederherstellung, unvollständige Operationen und Rollback liefern.
Ein Update interpretiert alte Snapshots nicht ungeprüft neu; Kompatibilität
nachweisen oder vor Wechsel kontrolliert leeren.

## 7. Verbindliche Freigabe

Alle erforderlichen nativen Laufzeitregressionen, Sysbox-Unit/Core/Integration/
Bats-Suites und CRIU-Core-Checkpoint/Restore-Prüfungen mit erhaltenen
Upstream-Assertions auf dem ausgewählten Paket/Komponentenstand bestehen.
Referenz und benannte importierte Overlays byte-identisch bewahren. Änderungen
als geordnete Patches; Fixture-Kompatibilität mit exakten Vorher-/Nachher-Hashes,
beibehaltenen Assertions und aktiviertem Vet deklarieren.

Erforderliche fehlgeschlagene oder nicht ausgeführte Assertions verhindern
Produktionsqualifikation. Ein Testskip wird nicht durch Umbenennung zum Pass.
Explizit nicht unterstützte Fähigkeiten bleiben ausgewiesen und gesperrt;
keine pauschale Sysbox-/OCI-Kompatibilität oder Plattformfreigabe behaupten.

Quellen, Patches, ausgelieferte Bytes, Plattform, unabhängige Builds und alle
Freigabeergebnisse in einem neuen überprüfbaren Profil verbinden. Das alte
Referenzprofil nicht umschreiben. Das Goal erst abschließen, wenn alle für
diesen Betrieb erforderlichen Arbeiten und Nachweise vorliegen.

## Arbeitsstand

- Baseline mit bisherigem Goal und Quellmanifest gesichert.
- Der vollständige Factory-Lifecycle mit OpenCode 2.0.27, echtem kostenlosem
  externem Modell und laufendem Vordergrund-Werkzeug besteht einmal, mit
  16 MiB privatem RAM, Dateiwatch und unabhängiger Ergebnis-/Exitverarbeitung
  in Quelle und Nachkommen. Gateway-Prozess und Verbindungen enden jeweils
  vor Capture. Geplanter und harter vollständiger Worker-Ersatz bestehen.
  Ein absichtlich verlorener Modellabschluss besteht mit bytegenauer
  Wiederholung der Inferenzanfrage und ohne zusätzliche Shell-Ausführung.
  Der frühere Fehlversuch vor Werkzeugstart bleibt als Fehler ausgewiesen.
- Ein exakt gehashter optionaler Dev-Containers-Patch vermeidet einen unnötigen
  Katalogabruf ohne Features. Beide vollständigen Beispiele und neun Prüfungen
  der originalen/gepatchten Feature-Kontrolle bestehen. Erste einzelne
  vorbereitete Starts mit korrigierter Factory-Gegenstelle: Factory
  23,8 → 18,9 s, minimal 9,1 → 3,3 s.
  Weitere Messungen vor Leistungsfreigabe; keine p95-Aussage.
- Der vorherige Factory-Wert enthielt einen nachgewiesenen Zehn-Sekunden-
  Timeout der Test-Gegenstelle bei einer ignorierten `stream-error`-Meldung.
  Geordneter Fixture-Patch und Regression erhalten alle bisherigen Assertions.
  Die Messkorrektur ist keine zusätzliche Laufzeitoptimierung.
- Der feste Vergleich unter
  `factorybox-consumer-start-comparison-88e95d80871c44afb590c575a76ac154`
  brach nach 14 vollständigen Erfolgen beim 15. Lauf ab. Beim Fork einer
  resumierten Factory-Quelle meldet CRIU einen PID-Konflikt. Der vollständige
  Fehlerdump bleibt erhalten; die Leistungsfreigabe ist nicht erteilt.
  Ein neuer CRIU-Build ergänzt ausschließlich lesende Fehlerdiagnostik.
  Seine drei festen vollständigen Factory-Läufe bestehen. Der seltene Fehler
  trat dabei nicht erneut auf; Ursache und Reparatur bleiben offen.
  Weitere Factory-Initialisierung wird gemessen. Speichergrenzen, Retention,
  Starttimeout, Dauerlauf und reduzierte Auslieferung sind noch nicht erledigt.
- Der Go-Worker zeichnet Fehler beim Dienststart vor dem Stoppen seiner
  Dienste auf. Begrenzte Prozess-/Thread-Daten enthalten die echten eigenen
  Kindprozesse; ursprünglicher Fehler und Bereitschaftsprüfung bleiben erhalten.
  Absichtlicher Start-Timeout und Dienstabbruch bestehen nativ. Ein vollständiger
  Factory-Lifecycle auf dem neuen Build besteht ebenfalls. Das ist Diagnostik,
  keine Erklärung oder Reparatur des historischen Startfehlers.
- Eine neue Kapazitätsanzeige und Aufnahmeprüfung lehnen neue Schreibarbeit
  unter der lokalen Freiplatzschwelle ab. Ein tatsächlicher Platzmangel-Test
  fand eine eingefrorene Quelle durch fehlenden Rückabwicklungsplatz. Eine
  vorab zugeteilte, besitzgeprüfte Rückabwicklungsreserve behebt diesen
  konkreten Fall nativ mit unverändertem RAM und altem Checkpoint. Das ist
  noch keine harte Store-Quote oder allgemeine Platzmangel-Freigabe.
- Ein weiterer realer Fehler bei gleichzeitigem Capture-Worker-Verlust und
  vollständig vollem Snapshot-Dateisystem ist reproduziert und korrigiert.
  Der Controller gibt seine exakt besessene Reserve vor den Rückabwicklungs-
  Belegen frei. Checkpoint und Pause bestehen den Fehlerfall mit laufender
  Originalquelle vor Entfernen des Platzdrucks und unabhängig brauchbarem
  altem Checkpoint. Die bisherigen Speicherfälle bestehen ebenfalls.
- [Fortschritt und genaue Belege](docs/production-progress.md). Das bestehende
  Referenzprofil bleibt unverändert; der neue Go-Worker ist noch nicht freigegeben.
- Die Fortsetzung in einem einzigen neuen Implementierungs-Chat ist mit den
  letzten sichtbaren Nutzerentscheidungen, Assistentenmeldungen, eingefrorenen
  Dokumenten und exakten Ergebnis-Hashes unter
  `artifacts/factorybox-chat-handoff-f58a26a3d19c47ba830a3a8cbeae16fb/` gesichert.
  Das vollständige Produktionsziel bleibt bestehen.
- Zwanzig vollständige aktuelle Factory-Läufe bestehen als Diagnose, ohne die
  seltenen Fehler oder die finale 100-Fälle-/24-Stunden-Abnahme zu ersetzen.
  Der isolierte Quotenversuch belegt anschließend, dass zusätzliche Btrfs-
  Untervolumes nicht automatisch durch die einfache Elternquote begrenzt sind.
  Ein neuer echter Sysbox-Gasttest bestätigt, dass ein ausdrücklich eingebundenes
  eigenes Btrfs-Verzeichnis solche Untervolumes zulässt. Auf seinem Overlay-
  Dateisystem wird die Operation abgelehnt. Beide normalen Schreibkontrollen,
  begrenzte Datenmenge und exaktes Aufräumen bestehen. Eine integrierte harte
  Speichergrenze bleibt deshalb offen; der Befund wird nicht als Freigabe gewertet.
- Ein festes 96-MiB-Dateisystem besteht den isolierten echten Platzmangel,
  die zweite Schreibablehnung, Rückabwicklung aus einer eigenen Reserve,
  Datenerhalt über erneutes Einhängen und die exakte Loop-Freigabe. Der erste
  fehlgeschlagene Versuch und seine geordnete Korrektur bleiben erhalten.
  Der daraus extrahierte Go-Baustein `internal/storefs` besteht vier native
  Prüfungen ohne Skips, einschließlich veränderter Image-/Mount-Identitäten,
  exklusivem Besitz und unsicherer Initialisierung. Die Verbindung zu Worker,
  Gäste-Speicherpfaden, Aufnahmeprüfung und Wiederherstellung nach Worker-Ersatz
  steht noch aus. Der neue Speicherweg ist nicht ausgewählt oder freigegeben.
- Der neue Speicherweg ist inzwischen ausdrücklich in Worker-Start, gesicherte
  Image-Identität und getrennte interne/Host-Aufnahmeprüfungen eingebunden.
  Ein vollständiger kleiner Sysbox-Test auf dem tatsächlich ausgelieferten Build
  besteht Checkpoint, unabhängigen Fork, gemessene Pause/Resume und echten
  Platzmangel durch Gastschreibarbeit. Externe Bind-Quellen werden vor Intent
  abgelehnt. Ein gesicherter Fehler zeigt erhaltene Dateiblöcke über einen
  Gast-Overlay-Alias nach Unlink; Retention darf freigegebenen Platz nicht aus
  gelöschten Namen ableiten. Alle Fehler und exakten Fixture-Korrekturen bleiben
  erhalten. Worker-Ersatz, vollständige Verbraucher, weitere Fehlerfälle und
  finale Abnahme auf diesem integrierten Pfad bleiben offen; Produktionsfreigabe
  und Auswahl des Backends bleiben ausdrücklich falsch.
