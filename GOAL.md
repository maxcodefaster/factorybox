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
- Geplanter und abrupter vollständiger Worker-Ersatz bestehen nun auch auf
  dem begrenzten Image mit 32 MiB privatem Zufalls-RAM und ausschließlich
  ausgelieferten CLI-Lifecycle-Aufrufen. Neue Epoche, identisches Image,
  falscher Recovery-Hash, exakte Wiederholung, unabhängige Nachkommen und
  Fork nach Resume bestehen. Doppelbesitz, geänderter Store-Besitzer und
  geänderte Kapazität werden vor Image-Veränderung abgelehnt; ein unsicheres
  Initialisierungsfragment bleibt byte-identisch erhalten. Native Bereinigung
  und Host-sysctl-Vergleich bestehen. Vollständige Verbraucher und finale
  Abnahme auf diesem Pfad bleiben offen. Checkpoint-/Epochen-Stilllegung ist
  weiterhin zu implementieren; die Regeln für Auswahl, Besitz und beobachtete
  Speicherfreigabe sind als vorgeschlagene Entscheidung gesichert.
- Eine erste Checkpoint-Stilllegung samt dauerhaften Recovery-Auswahlen ist
  implementiert, aber nicht freigegeben. Auf Build `f09e41168c1240bba4422c263795a7f9`
  bestehen der vollständige Stilllegungstest mit echtem Controller-SIGKILL,
  fremdem Eintrag beim Wiederanlauf, unabhängigen Nachkommen und beobachtetem
  Kapazitätsgewinn sowie ein zusätzlicher RAM-Aktor auf geteilter unveränderlicher
  Basis. Platzmangel und geplanter/abrupter Worker-Ersatz bestehen erneut.
  Der erforderliche ursprüngliche Basis-Test mit `sleep 300` schlägt weiter fehl:
  CRIU meldet einen unterbrochenen Warteaufruf, und das gepinnte BusyBox 1.37.0
  wiederholt seinen einzelnen `nanosleep` nicht. Reines Einfrieren/Auftauen erhält
  den Prozess. Der Fehler, Exitstatus 0, unverbrauchte Restzeit, Objektcode und
  exakte Fixture-Änderungen sind unter
  `factorybox-checkpoint-retirement-notes-890fbf4221d14ee3b7cf7a13859d6ea2` gesichert.
  Die vollständige Stilllegungs-Suite bleibt fehlgeschlagen. Warteaufruf-Verhalten,
  Epochen-Stilllegung und alle übrigen Abnahmen bleiben offen.
- Der Wartefehler ist nun auch ohne BusyBox durch ein natives AMD64-Programm
  mit tatsächlich schreibgeschützter Zeitangabe und ohne Restzeit-Zeiger
  reproduziert. Die neue erforderliche Regression prüft außerdem Register,
  Flags, Stack, unveränderte Zeitangabe und die ursprüngliche Ablaufzeit von
  Quelle und Fork. Sie scheitert auf dem vorhandenen Paket bereits am vorzeitig
  beendeten Quellprozess; spätere Assertions bleiben damit ungeprüft.
  Ein rein lesender temporärer BPF-Iterator kann auf diesem Kernel den genauen
  Restart-Typ, Clock und Ablaufzeit auslesen. Geburtszeit, PID-Namespace-Trennung
  und Freigabe jedes eigenen Programms sind nativ geprüft. Die aktuellen
  Stilllegungstests mit RAM-Aktoren bestehen weiter; beide Warte-Tests schlagen
  fehl. Belege und geordnete Fixture-Hashes liegen unter
  `factorybox-timer-restart-diagnosis-b0e8c3c680174fd4853859b109b97073`.
  Eine Laufzeitkorrektur oder ausgewählte neue Kernel-Abhängigkeit ist damit
  noch nicht geliefert. Das vollständige Produktionsziel bleibt aktiv.
- Ein isolierter Timer-Versuch erhält jetzt Register, Flags, schreibgeschützte
  Zeitangabe und ursprüngliche Ablaufzeit über Checkpoint, erneuten ausgelieferten
  CLI-Checkpoint, Fork sowie Pause/Resume des Forks. Der Versuch verwendet einen
  einzelnen nativen Thread, einen NULL-Restzeit-Zeiger und Zeit-Namespace-
  Offsets von null. Er ist noch keine integrierte Runtime-Korrektur.
  `factorybox-checkpoint-retirement-59f44517b59b464eb3b6114bbd63394d` enthält den
  erfolgreichen Nachweis ohne Exit-Tracer. Tatsächliche Gast-Mounts ersetzen
  unzuverlässige negative Lookups auf dem äußeren Overlay im Test; alle
  ursprünglichen Assertions und das erforderliche Testprogramm bleiben erhalten.
  Ein neuer kleiner Pause-Fall zeigte außerdem verzögerte anonyme Speicher-
  abrechnung. Build `2b11bea0a63c4136a18d41bbe211af80` bestätigt jetzt vor dem
  Pause-Abschluss die leere eigene cgroup und den tatsächlich gemessenen
  Nullstand, begrenzt auf drei Sekunden und den Aufrufer-Kontext. Speichergrenze,
  Gast-ENOSPC und Snapshot-RAM-Freigabe bestehen. Die vollständige erforderliche
  Stilllegungs-Suite `factorybox-checkpoint-retirement-abce758b443345b08446ba1593a6298a`
  bleibt mit zwei bestandenen und zwei fehlgeschlagenen Tests gesperrt.
  Mehrere Threads, Signale/Restzeit-Zeiger, Zeit-Offsets und sichere Lebensdauer
  der Fortsetzung sowie alle übrigen Produktionsabnahmen bleiben offen.
  Geplanter und abrupter Worker-Ersatz bestehen auf den neuen ausgelieferten
  Bytes ebenfalls. Exakte Fixture-Hashes, erfolgreich rekonstruierte geordnete
  Patches und der geprüfte Belegindex liegen unter
  `factorybox-timer-continuation-notes-c3b2393e0dc44ad19708a61cd15b2c02`.
- Der aktuelle ausgelieferte Kandidat `2fca9b438e664527a211a44df38bd0f7`
  besteht die bisherigen Stilllegungs-, Speichergrenzen- und Worker-Ersatzfälle,
  hat aber einen nachgewiesenen Restzeit-Kopierfehler. Ein Signal-Handler, der
  seine eigene Restzeit-Seite schützt, führt im nativen Kontrollfall zu EINTR,
  im vorbereiteten Kandidaten zu EFAULT. Der unveränderte finale Test bestätigt
  das in `factorybox-checkpoint-retirement-98bebdb146ae463e87146c0738fa442f`.
  Ein ausschließlich im Test ausführbarer Versuch mit echtem Kernel-Wait
  besteht sechs Fälle einschließlich Fork, Resume und eingefrorenem Signal.
  `factorybox-checkpoint-retirement-7c294dbd14b34613823c82854cb26126`
  belegt auch den tatsächlich gestarteten Kernel-Timer und eine native
  eingefrorene Vergleichskontrolle. Die Runtime ist noch unverändert.
  Aktivierung vor Freigabe der Quelle und wiederhergestellter Tasks,
  Fehlerreparatur und alle übrigen Produktionsabnahmen bleiben offen.
- Kandidat `d8cc5e7841bb4b6190fbb6331ff7fc5f` enthält jetzt ein optionales,
  versiegeltes Backend-Protokoll, das wiederhergestellte Tasks vor CRIUs
  Freigabe in ihrer eigenen cgroup hält. Der tatsächlich laufende native
  Zähler bleibt über Bestätigung und CRIU-Detach bis zum expliziten Auftauen
  unverändert. Fork, Resume, beide Verbindungsabbrüche und eine falsche
  Bestätigung bestehen in
  `factorybox-checkpoint-retirement-47c230423bc2402f939e1039b1f3723d`.
  Unbestätigte Operationen behalten ihre Belege; deren Löschung wird geprüft
  verweigert. Die vier bisherigen Stilllegungstests bestehen ebenfalls.
  Der öffentliche Lifecycle nutzt diese Sperre noch nicht. Die Timer-
  Aktivierung muss vor dem Auftauen integriert und unter Fehlern sowie
  wiederholtem Capture/Recovery geprüft werden. Der Restzeit-Kopierfehler und
  das vollständige Produktionsziel bleiben offen.
- Kandidat `edd5cff5a26a4488b2fd11de136434ac` nutzt die Restore-Sperre jetzt
  im ausgelieferten Fork-/Resume-Lifecycle. Ein zweiter unabhängig bewachter
  Mutator aktiviert den ursprünglichen Kernel-Wait vor Freigabe der Quelle
  beziehungsweise des wiederhergestellten Tasks. Der Restzeit-Kopierfehler
  besteht im neuen Vier-Fälle-Test einschließlich Fork und Resume nicht mehr;
  native Kontrolle, ursprüngliche ABI-Prüfungen und 250-ms-Grenze bleiben.
  Alle vier Signalgrenzen sowie die vollständigen vier Stilllegungstests
  bestehen auf denselben Paketbytes. Ein zunächst abgelehntes Signal beim
  erneuten Capture bleibt als negativer Beleg erhalten; die Korrektur gilt
  ausschließlich für den exakt verifizierten privaten nativen Timer.
  Geordnete Quell-/Fixture-Patches, exakte Vorher-/Nachher-Hashes und der
  geprüfte Index liegen unter
  `factorybox-timer-activation-notes-e5db5e8880b4482097744cd35aadf8f2`.
  Unterbrochene Aktivierungs-/Mutationsreparatur, breitere Signalzustände,
  Lebensdauer alter privater Seiten sowie sämtliche übrigen Freigabearbeiten
  bleiben offen. Das vollständige Goal bleibt aktiv; Produktionsfreigabe false.
- Auf denselben Kandidatenbytes bestehen nun auch geplanter und abrupter
  Ersatz des begrenzten Workers, die tatsächliche Gast-Speichererschöpfung,
  18 unveränderte CRIU-Fälle mit je zwei Restore-Zyklen, sämtliche ausgewählten
  Sysbox-Go-Modulsuites und die bisherigen Bats-Dateien für Lifecycle/systemd.
  Die 18 ursprünglichen Go-Skips und zwei ursprünglichen systemd-Skips bleiben
  ausdrücklich sichtbar. Der private Offline-Build nutzt dieselbe gepinnte
  DNS-Datei wie der Komponentenbuild; ein vorheriger Vorbereitungs-Timeout
  bleibt fehlgeschlagen erhalten. CRIU-Core erhält den exakt geprüften alten
  Basis-Lock explizit, ohne die CRIU-Rezeptprüfung abzuschwächen.
  `factorybox-activation-gates-notes-f536ae18444e48369e4f946b96d31372`
  bindet Ergebnisse, Paketbytes und Quellpatches. Eine Codeprüfung zeigt noch
  fehlende Timer-Aktivierung in den bestätigten Abbruch-/Cure-Pfaden; deren
  Freigabe einer vorbereiteten Fortsetzung muss mit echtem ENOSPC nativ geprüft
  und korrigiert werden. Vollständiges Produktionsziel weiterhin aktiv.
- Kandidat `37dd732100cd45be93cc7a123ee39f1a` korrigiert jetzt den bestätigten
  Capture-Abbruch. Der neue native Test reproduziert auf dem vorherigen Paket
  nach tatsächlichem ENOSPC den falschen EFAULT-Rückgabewert; auf dem neuen Paket
  bestehen Kontrolle und Rückabwicklung mit ursprünglichem EINTR, ABI und
  Unterbrechungs-Restzeit. Die Aktivierung läuft vor der Freigabe und erhält
  einen eigenen Eltern-Identitätsbeleg. Orphan-/delegierte Cure-Pfade verweigern
  ohne bestätigten Timer-Abschluss das Auftauen. Zehn portable Fälle prüfen
  diese Belegregel. Alle elf bisherigen nativen Regressionsgates bestehen auf
  den neuen Paketbytes erneut; negative/positive Abbruchfälle, exakte Quellpatches
  und Belege liegen unter
  `factorybox-timer-abort-notes-ba5e6e083bba490185420f3d0a2b1c12`.
  Native Verlustgrenzen und automatische Reparatur einer
  unvollständigen Timer-Mutation bleiben offen. Das vollständige Goal bleibt
  aktiv, Produktionsfreigabe false.
- Ein eigener ausgelieferter CLI-Pause-Test belegt nun eine Timer-Orphan-Grenze:
  echter Controller-/Worker-SIGKILL, exaktes Reap und anschließende Entscheidung
  des ursprünglichen Guards. Die Quelle bleibt trotz ausstehendem SIGUSR1
  eingefroren und untraced; der Handler läuft nicht, die unbestätigte Pause
  bleibt erhalten und ihre Löschung wird verweigert. Der anfängliche Fixture-
  Wait-Fehler, beide erfolgreichen Versuche, Output-Leserechte und exakte Patches
  liegen unter `factorybox-timer-orphan-notes-77fc993985844b24af1e9a0695dd1311`.
  Runtime- und Komponentenbytes bleiben unverändert. Guard-Verlust, automatische
  Mutationsreparatur und sämtliche weiteren Freigabearbeiten bleiben offen.
- Der komplementäre Guard-Verlusttest besteht auf denselben Paketbytes:
  tatsächlicher Guard-/Capture-Worker-SIGKILL nach Prozess-Capture, überlebender
  ursprünglicher Controller, exakt erhaltene eingefrorene und untraced Quelle
  mit ausstehendem SIGUSR1. Timer-Code und Datensatz bleiben unverändert;
  exakte Pause-Wiederholung und Löschung werden verweigert. Der ursprüngliche
  Orphan-Fall besteht nach dem Teilen des Testaufbaus erneut. Fehlgeschlagene
  Fixture-Versuche, alle erhaltenen Assertions und geordnete Patches liegen
  unter `factorybox-timer-guard-loss-notes-df133f48e20842a3b4327b955e72be8e`.
  An dieser Grenze fehlt ein Cure-Auftrag; Verlust nach dessen Annahme,
  automatische Mutationsreparatur und das vollständige Produktionsziel bleiben
  offen.
- Die vorhandenen vollständigen Verbraucher-Fixtures können jetzt mit exakt
  deklarierten Pfadpatches im begrenzten Store laufen; Originale, Assertions,
  Vet und Ressourcen-/Zeitgrenzen bleiben erhalten. Der erste minimale Lauf
  besteht alle acht Lifecycle-Phasen, scheitert danach aber beim Worker-Stop.
  Ein kleiner nativer Vergleich belegt die Ursache: leerer Worker endet mit 0,
  genau eine vorbereitete Basis hinterlässt ihren schreibgeschützten Self-Bind
  und führt zu EBUSY/Exit 1. Der Gesamtlauf bleibt fehlgeschlagen.
  Die Sicherung berücksichtigt nun explizit die bekannte Imagegröße plus
  Tar-Zugabe, ohne das physische Artefaktbudget zu erhöhen. Belege und exakte
  Patches: `factorybox-bounded-consumer-notes-f11aa1b429604122afcad7bad16e89cc`.
  Sauberer Stop, vollständige begrenzte Factory-/Theia-/Agenten- und
  Worker-Verlustläufe sowie sämtliche weiteren Freigabegates bleiben offen.
- Der begrenzte minimale Verbraucher besteht auf dem neuen Stop-Kandidaten
  `f4f3b99388ca476d9b2d18592bd033d6` erstmals vollständig mit sauberem Exit 0
  und Cleanup. Der neue Pfad prüft bestätigte Löschung, sperrt die alte Epoche
  und gibt exakte Checkpoint-Aliase vor ihren kanonischen Basismounts frei.
  Ein Dateisystem-Trace belegt den zwischenzeitlichen ENOENT-Fehler: interne
  Timer-Guard-Generationen wurden als veröffentlichte Checkpoints behandelt.
  Der neue native Metadaten-Test reproduziert diesen Fehler und besteht nach
  der Korrektur alle 13 Fälle einschließlich zwölf Verweigerungen.
  Pausierte Epochen, wiederhergestellte Basislayouts, sämtliche übrigen
  Regressionsgates und das vollständige Produktionsziel bleiben offen.
- Der vollständige Factory-/Theia-/OpenCode-Fixture-Lauf im begrenzten Store
  besteht nun auf Paket `10aaf182ad584ceaabcd8411cdaadf82` einschließlich
  aller acht Lifecycle-Phasen, Exit 0 und Cleanup. Die vollständige RAM-Prüfung
  bleibt erhalten. Frühere interne Stop-Budgets von fünf und zwölf Sekunden
  scheitern nach erfolgreicher Funktion; der aktuelle Pfad erhält 30 Sekunden
  für die Prüfung und höchstens 45 Sekunden äußere Stop-Gnadenfrist, begrenzt
  durch die verbleibende ursprüngliche Lifecycle-Frist von 420 Sekunden.
  Alle 13 nativen Kern-Regressionsgates bestehen auf denselben Paketbytes.
  Externe echte Modellabnahme, vollständiger Verbraucher-Worker-Ersatz mit
  vorbereiteter Basis, sämtliche übrigen Freigabearbeiten und das Gesamtziel
  bleiben offen. Die Modell-Fixture ersetzt keinen externen Anbieterbeleg.
- Der vollständige geplante begrenzte minimale Worker-Wechsel besteht auf
  `c2466d1f9df74c30be742170ac867948` in
  `factorybox-consumer-worker-loss-805b260a3a1e4d3d97ae935cda88fb1d`.
  Die bestätigte Pause bleibt erhalten; erst die dauerhafte Epochensperre
  erlaubt die Freigabe der exakt gestoppten Runtime und ihrer leeren cgroup.
  Recovery liest nur den gebundenen Zustand und erhält unabhängige Inputs.
  Auch der abschließende Stop berücksichtigt die wiederhergestellte Basis
  außerhalb des Basisregisters. Beide Worker enden mit 0, Cleanup besteht.
  Beide vorherigen Fehlerbelege bleiben gesichert. Das erste Abbild bleibt
  vollständig; das durch erfolgreichen Ersatz abgelöste Recovery-Abbild
  `6056f81a` ist mit vollständiger Verifikation und Stilllegungsbeleg bewusst
  nicht mehr rekonstruierbar.
  Das mit korrigierter Test-Fixture neu gebaute Paket
  `c8578b8ae98a47f392bc050c79274346` enthält dieselbe CLI. Darauf bestehen
  alle 13 nativen Kern-Regressionsgates, acht Stop-Schutzfälle sowie geplanter
  und abrupter minimaler Verbraucher-Ersatz und der normale Acht-Phasen-Lifecycle.
  Ein unabhängiger Offline-Build reproduziert Paket und Image exakt.
  Auch vollständiger Factory-/Theia-/OpenCode-Fixture-Ersatz und normaler
  Lifecycle bestehen auf diesen Bytes. Alle acht Verbraucherfälle sind in
  `factorybox-paused-consumers-72cb8cb207884bc4a22bfdb4c8444908` gebunden.
  Beide unvollständigen Pausen werden zweimal verweigert, danach wird der
  frühere bestätigte Zustand vollständig wiederhergestellt. Die Modell-Fixture
  ersetzt keine externe Anbieterabnahme. Unterbrochener Live-Drain, veränderte
  Loop-Zuordnung, große Exit-Belege und sämtliche weiteren Produktionsabnahmen
  bleiben offen. Das vollständige Goal bleibt aktiv.
- Ein neuer vollständiger minimaler Test erzwingt beim Worker-Ersatz den Wechsel
  von Loop 0 auf Loop 1. Das bisherige Paket scheitert am gespeicherten
  Kernel-Device, obwohl die vollständig verifizierte Offline-Inspektion UUID,
  Inode, Rechte, Eigentümer sowie mtime/ctime unverändert nachweist.
  Kandidat `3d7bacfa1646433da4f0ce802fef80ac` bindet die Übersetzung deshalb
  an die exakte persistente Worker-Epoche und das aktuell verifizierte Abbild.
  Alle übrigen Identitäts-, Inhalts- und Mount-Prüfungen bleiben erhalten.
  Der vollständige minimale Ersatz besteht damit einschließlich neuem Fork
  und sauberem Stop. 26 negative Identitätsfälle und beide nativen Basis-Tests
  bestehen ebenfalls. Quellen, Ergebnisse, Logs und vollständige Diagnose des
  ursprünglichen Fehlers bleiben gesichert. Nach der breiten bestandenen Abnahme
  ist sein großes Abbild mit vollständiger Stream-Verifikation und Beleg
  `factorybox-storage-retirement-6ab725eb0b084e5fae1ac6ddaa93beb0`
  bewusst nicht mehr rekonstruierbar.
  Der unabhängige Nachbau reproduziert Paket und Image exakt. Alle 13 nativen
  Kern-Regressionsgates, acht Stop-Schutzfälle und zwölf vollständige
  Verbraucherfälle bestehen auf diesen Bytes. Dazu gehören die acht bisherigen
  Fälle und alle vier Kombinationen aus minimal/Factory und geplant/abrupt mit
  erzwungenem Loop-Wechsel. Factory enthält separate Theia und die lokale
  OpenCode-Fixture. Der Nachweisindex ist
  `factorybox-device-recovery-notes-84957ba0430f45e0810ff6a869735fe3`.
  Echter unterbrochener Drain und sämtliche weiteren Produktionsanforderungen
  bleiben offen; das vollständige Goal bleibt aktiv.
- Ein externer Beobachter trifft jetzt die tatsächliche Drain-Sperre und beendet
  ausschließlich den exakt geprüften eigenen Worker per pidfd-SIGKILL. Der
  passende Intent bleibt nach bestätigtem Prozessende unverändert erhalten,
  der Abschluss fehlt. Alle Beobachter-Handles sind vor dem Ersatz geschlossen.
  Minimal und Factory bestehen danach vollständig; beide bestehen zusätzlich
  mit erzwungenem Loop-Wechsel. Alle vier Fälle liegen unter
  `factorybox-interrupted-drain-consumers-d128a70160f44b85a831aae4fddb5d50`.
  Runtime und Komponenten bleiben unverändert. Der Nachweisindex ist
  `factorybox-interrupted-drain-notes-ffc74b3b9e414ac1bc8f0948c613462a`.
  Der erste fehlgeschlagene Python-API-Vorversuch bleibt erhalten; der korrigierte
  Beobachter verweigert falsche/veraltete Prozessidentitäten und signalisiert im
  positiven Vorversuch ausschließlich den eigenen Kindprozess. Dies belegt
  Prozessverlust im selben Boot, keinen Stromausfall oder Host-Neustart.
  Große Exit-Belege, sämtliche übrigen Abnahmen und das vollständige Goal
  bleiben offen und aktiv.
- Der echte Größen-Test importiert 540 kleine Basen über die öffentliche CLI.
  Ein Beleg oberhalb von 4 MiB wird vor jeder kanonischen Unmount-Operation
  verweigert; nach dem Ersatz sind sämtliche Daten und Datensätze unverändert.
  Ein zweiter Test trifft einen Intent mit nur 15 Byte Reserve und zeigt einen
  Fehler: Alle 540 Basismounts werden freigegeben, erst der größere Abschluss
  scheitert. Intent und vollständiger Fehler-Store bleiben erhalten, Nutzdaten
  sind unverändert. Der Kandidat prüft jetzt vor der Sperrveröffentlichung auch
  die maximal große Abschlussform mit demselben Größenprüfer.
  Das neue Paket `5c281a6c3d6d4f08808be45deb0b75f1` und sein unabhängiger
  Nachbau stimmen exakt überein. Beide nativen Größenfälle bestehen: kein
  kanonischer Unmount, beide Sperrdateien fehlen, alle 540 Basen unverändert
  und der neue leere Worker stoppt mit 0. Alle 13 nativen Kerngates, acht
  Stop-Schutzfälle und beide Basis-Tests bestehen ebenfalls. Die zwölf
  vollständigen Verbraucher und alle vier echten Drain-Abbrüche bestehen auf
  diesen neuen Bytes. Der Nachweisindex ist
  `factorybox-ledger-notes-cd1266fe9219423dbd9b5651d14e85fd`; der ursprüngliche
  vollständige Fehler-Store bleibt erhalten und wird vollständig verifiziert.
  Begrenzte Aufnahme neuer Arbeit und Epochen-Stilllegung bleiben getrennte
  offene Anforderungen. Das vollständige Goal bleibt aktiv.
