# Factorybox für den Factory-Piloten

Status: Am 2026-10-21 bestätigt und als aktives Ziel gestartet.
Am 2026-10-22 für den vereinbarten lokalen Factory-Piloten abgeschlossen:
[Abnahmebericht](docs/pilot-validation.md) und
[geprüftes Pilot-Referenzprofil](upstream/amd64-pilot-profile.json).
Funktionale Pilotabnahme und das gemessene Geschwindigkeitsziel sind erfüllt;
eine vollständige Produktionsfreigabe bleibt ausdrücklich offen.
Die abgeschlossene lokale 0.1-Abnahme bleibt in
[amd64-validation.md](docs/amd64-validation.md) nachvollziehbar.
Der Arbeitsstand und die noch offenen Nachweise stehen in
[pilot-progress.md](docs/pilot-progress.md).

## Ziel und Einsatzgrenze

Factorybox als eigenständig startbaren Linux-AMD64-Worker für die anschließende
Integration in den Factory-Piloten ausliefern. Die vorhandenen Beispiele sind
die verbindliche Testumgebung. Die Factory-Repositories und das historische
Experiment bleiben unverändert; eine echte Factory-Installation oder externe
Produktionszugänge sind keine Voraussetzung für die Abnahme.

Die nächste Etappe verbindet nachgewiesene Geschwindigkeitsverbesserungen mit
Agentenfortsetzung und Wiederherstellung vollständig gespeicherter
Pausezustände nach einem kompletten Worker-Neustart auf demselben Rechner.
IDEs, Dev Containers und Agenten bleiben optionale Verbraucher. Sysbox-CLI- oder
API-Kompatibilität ist weiterhin keine Anforderung.

Die native Abnahme erfolgt zuerst auf dem vorhandenen Linux-AMD64-Rechner mit
dem ausdrücklich geprüften Kernel, 4096-Byte-Seiten, cgroup v2 und Speicherpfad.
Der Worker dokumentiert und prüft seine Voraussetzungen. Andere Kernel,
Dateisysteme oder Zielrechner benötigen eigene native Nachweise.
Integrationsfähigkeit ist keine pauschale Plattformfreigabe.

## 1. Bisherigen Lifecycle vollständig erhalten

Das minimale Dev-Container-Beispiel und die Factory-Komposition mit separater
Theia, privatem Docker, Compose und Dev Containers müssen mit dem tatsächlich
ausgelieferten Runtime-Paket weiterhin bestehen:

- Start und Status, vollständiger Checkpoint und unabhängiger RAM-/Dateisystem-Fork.
- Unterschiedliche RAM- und Dateiänderungen in Quelle und Fork.
- Checkpoint und Fork einer bereits geforkten Quelle.
- Löschen der ursprünglichen Quelle bei weiterhin brauchbaren Nachkommen.
- Pause mit gestoppten Sandbox-Prozessen und nachgewiesener Freigabe des
  Workload-RAMs sowie der residenten Snapshot-RAM-Daten.
- Resume mit erhaltenem Zustand, erneuter Checkpoint und Fork nach Resume.
- Exakte, wiederholbare Löschung ausschließlich eigener Ressourcen.
- Weiterarbeit der separaten Theia mit dem ausgewählten Workspace.

Die bisherige Referenz bleibt als unveränderter Vergleich verfügbar.
Zusätzliche optimierte Verbrauchervarianten ersetzen diese Abnahme nicht.

## 2. Drei konkrete Optimierungsfälle untersuchen

### A. Wiederholtes vollständiges Lesen und Prüfen

Die vollständigen Lesevorgänge im Kopierprozess, im Controller sowie bei Fork
und Resume einzeln zuordnen und zeitlich erfassen. Innerhalb derselben
geschützten Operation prüfen, ob bereits erbrachte vollständige Nachweise sicher
weiterverwendet werden können.

Eine Änderung muss Manipulation, Quellenänderung, fremde Mounts/Inodes,
Prozessabbrüche, unvollständige Ergebnisse und unsichere Wiederholungen
weiterhin erkennen. Eine gespeicherte Erfolgsmarkierung oder ein unveränderter
Zeitstempel ersetzt keine Inhalts- und Identitätsgarantie.

### B. Unveränderliche Docker-Daten wiederverwenden

Prüfen, ob nachweislich unveränderliche Image-Daten aus dem wiederholt
vollständig zu sichernden Docker-Zustand herausgelöst werden können.
Veränderlicher Docker-, Anwendungs- und Workspace-Zustand bleibt pro Fork
unabhängig. Quelle und ursprünglicher Worker müssen nach erfolgreicher
Veröffentlichung eines unabhängigen Nachkommen entbehrlich sein.

Dies ist ein zu qualifizierender Ansatz. Die Speicherersparnis oder ein
vollständiger Geschwindigkeitsgewinn sind noch nicht bewiesen. Die vorhandene
VFS-Komposition bleibt der Vergleich, bis eine vollständige Ersatzkomposition
alle erforderlichen Gates besteht. Alte Overlay2-, SHM-, Lazy-/Delta- oder
Lesehinweis-Kandidaten werden dadurch nicht automatisch ausgewählt.

### C. Vorbereitung wiederverwenden

Eine bereits vollständig importierte und geprüfte Basis für weitere Starts
wiederverwenden. Einmalige Vorbereitung pro Worker beziehungsweise Basis,
inkrementelle Vorbereitung und die anschließende Nutzerwartezeit getrennt
ausweisen. Vorbereitung darf keine versteckte Arbeit oder ungeprüfte Daten
hinterlassen.

Bereits umgesetzte Reflink-Anforderungen, Quelldigest-Wiederverwendung,
unveränderliche Basis, wiederverwendete Lesepuffer und gebündelte
Dauerhaftigkeitsbarrieren gelten als Ausgangspunkt, nicht als neue Gewinne.
Frühere erfolglose Mikrooptimierungen werden nur mit einer neuen konkreten
Begründung und unterscheidbarem Nachweis erneut untersucht.

Für jeden Ansatz vorab Hypothese, Gleichwertigkeitsprüfung und begrenzten
Vergleich festlegen. Unwirksame, unsichere oder unentschiedene Änderungen
verwerfen und das Ergebnis erhalten. Nur nachgewiesene Verbesserungen gelangen
in die ausgewählte Komposition.

## 3. Geschwindigkeit ohne schwächere Garantien nachweisen

Mit unveränderten Verbraucher- und Daten-Eingaben sowie denselben
Ressourcenobergrenzen eine frisch gemessene 0.1-Baseline und die finale
Komposition zeitlich verschachtelt vergleichen. Mindestens fünf vollständige
vergleichbare Beobachtungen pro Konfiguration erhalten. Reihenfolge,
Vorbereitung, Cache-Bedingungen, CPU-, I/O- und Speicherdruck sowie Ausfälle
dokumentieren; keine globalen Cache- oder Systemeinstellungen verändern.

Arbeitsziel ist mindestens 50 Prozent weniger Medianzeit für Checkpoint,
Pause und Fork bis zur brauchbaren Anwendungsantwort, jeweils gegenüber der
zugehörigen Phase der gemessenen Baseline. Fork-Zeiten aus fertigen Checkpoints
und die Summe aus neuem Checkpoint plus Fork getrennt ausweisen. Das Ziel ist
noch keine bewiesene Eigenschaft oder Latenzzusage.

Start, Resume und die bisherigen Lifecycle-Phasen des minimalen Beispiels
dürfen im gemessenen Median nicht um mehr als das größere Maß aus 20 Prozent
und 250 Millisekunden langsamer werden. Eine ausgeschöpfte oder verfehlte
Messgrenze darf nicht durch veränderte Eingaben oder höhere Limits verdeckt
werden.

Alle Einzelwerte, Mediane, Spannweiten und fehlgeschlagenen Läufe veröffentlichen,
einschließlich Vorbereitung, retained memory und erstem brauchbaren
Anwendungs-/Werkzeugergebnis. Aus dieser kleinen Stichprobe keinen p95 ableiten.
Das Halbierungsziel separat bewerten und bei Nichterreichen ausdrücklich offen
lassen. Funktionale Erfolge ersetzen den fehlenden Geschwindigkeitsnachweis nicht.

## 4. Laufenden Agenten und Werkzeugprozess fortsetzen

Den im Beispiel vorbereiteten, genau gepinnten OpenCode-Prozess mit einem
kontrollierten lokalen Modell-Testgegenüber tatsächlich einen Werkzeugaufruf
ausführen lassen. Reine Erreichbarkeit der Agenten-API reicht nicht.

Folgende Fälle nativ prüfen:

- Checkpoint/Fork während eines laufenden lokalen Werkzeugprozesses.
- Pause/Resume während dieses Werkzeugprozesses.
- Pause, kompletter Worker-Neustart und anschließendes Resume während dieses
  gespeicherten Werkzeugzustands.
- Unabhängige Fortsetzung und unterschiedliche RAM-/Dateiänderungen in Quelle
  und Fork; auch ein Nachkomme nach Löschen der ursprünglichen Quelle bleibt brauchbar.
- Das Werkzeug beendet sich mit dem erwarteten Resultat und Exitstatus;
  der Agent übernimmt dieses Resultat und setzt seinen Ablauf korrekt fort.
- Terminal und Browser dürfen sich neu verbinden. Der unterstützte Start-
  und Reconnect-Pfad darf Agenten- oder Werkzeugzustand nicht still verlieren.

Die bisher fehlgeschlagene angehängte Exec-/Sitzungstopologie erhält einen
gezielten Regressionstest und eine explizite Behandlung. Der unterstützte
Dienst-/Reconnect-Pfad muss bestehen. Eine Garantie für beliebige fremde
interaktive TTY-/Exec-Sitzungen wird daraus nicht abgeleitet.

Externe Modellanbieter, externe Gesprächspartner und deren Nebenwirkungen
werden nicht mitgeforkt. Echte Modellinferenz und Provider-Wiederanbindung
bleiben zusätzliche Pilotprüfungen. OpenCode bleibt Testverbraucher und wird
keine Abhängigkeit der Runtime.

## 5. Bestätigten Pausezustand nach Worker-Neustart wiederherstellen

RAM-Abbild, vollständiges benötigtes Dateisystem, unveränderliche Basisdaten
und Herkunfts-/Operationsdaten auf unabhängigem dateibasiertem Speicher
dauerhaft veröffentlichen. Persistente und flüchtige Worker-Daten ausdrücklich
trennen und dokumentieren.

Für beide vorhandenen Beispiele folgende Fälle prüfen:

- Erfolgreich pausieren, gesamten Worker einschließlich Sysbox-Diensten
  beenden und entfernen, einen frischen Worker auf demselben Rechner mit
  demselben persistenten Datenbestand starten und den Zustand fortsetzen.
- Dasselbe nach abruptem Verlust des Workers, nachdem Pause bereits
  erfolgreich bestätigt war.
- Der neue Worker darf keine alten Prozesse, offenen Deskriptoren oder
  weiterlebenden Hilfsdienste zur RAM-Wiederherstellung benötigen.
- Nach Wiederherstellung erneut checkpointen, forken, unabhängig verändern,
  die ursprüngliche Quelle löschen und die eigenen Ressourcen exakt bereinigen.
- Wiederholte Wiederherstellungsanfragen erzeugen keine unbemerkten Duplikate.
  Unsichere oder unterbrochene Versuche werden nachvollziehbar abgeglichen
  beziehungsweise sicher zurückgewiesen.
- Fehlende oder veränderte RAM-Dateien, Dateisystemdaten, Metadaten,
  Komponentenidentitäten und fremde Ressourcen werden erkannt. Andere
  Umgebungen dürfen weder übernommen noch gelöscht werden.
- Ein Worker-Verlust während einer noch nicht bestätigten Pause darf keinen
  erfolgreichen vollständigen Snapshot vortäuschen oder frühere bestätigte
  Snapshots beschädigen.

Die Zusage gilt für vollständig bestätigte Pausezustände bei erhaltenem
Speicher auf demselben Rechner und gleicher qualifizierter Plattform.
Ungespeicherter RAM laufender Umgebungen, Host-Neustart, Rechner-/Datenträgerverlust,
Cross-Node-Migration und Wiederherstellung auf anderer Hardware gehören nicht
zu dieser Abnahme. Keine Neustart- oder Migrationseigenschaft vor dem
entsprechenden tatsächlichen Test als unterstützt melden.

## 6. Integrationsfähigen Worker ausliefern

Einen reproduzierbar gebauten Docker-Worker mit genau gepinnten Sysbox-/CRIU-
Komponenten und einer stabil beschriebenen, versionierten lokalen
CLI-/JSON-Steuerung liefern. In diesem Repository ein ausführbares Beispiel
für Start, Pause, Worker-Neuerzeugung, Resume, Fork und Löschen bereitstellen.

Der Integrationsvertrag beschreibt insbesondere:

- Plattform, Worker-Rechte, Ressourcenlimits und unterstützten Speicher.
- Persistente Mounts, flüchtige Verzeichnisse und Speicherbedarf.
- Bereitschaft, Zustandsabfragen, Operationsidentitäten, Wiederholungen,
  Fehlerzustände und Wiederherstellung.
- Schutz der administrativen Steuerung und getrennte Worker pro Kunde oder
  Vertrauensbereich.
- Genaue Komponenten-/Quellidentitäten, Diagnoseinformationen,
  reproduzierbaren Neuaufbau und bekannte Grenzen.

Die Factory-Anbindung selbst erfolgt anschließend im Piloten. Das
Beispiel verwendet denselben ausgelieferten Worker und dieselben
Steuerkommandos. Eine öffentliche Netzwerk-API, ein Kubernetes-Operator,
beliebige Cluster-Unterstützung und öffentliche Veröffentlichung sind nicht
Bestandteil dieses Ziels.

## 7. Abnahme und Nachweise

Die ausgewählte Komposition muss die betroffenen nativen Regressionen,
beide vollständigen Beispiele, die neuen Agenten-/Neustartfälle,
die erforderlichen Sysbox-Unit/Core/Integration-Suiten und CRIU-Core-
Checkpoint/Restore-Prüfungen bestehen. Upstream-Assertions und Vet bleiben
erhalten. Originale Skips bleiben einzeln sichtbar; neue Fehlschläge oder
unbegründete neue Skips blockieren die Pilotabnahme. Bestehende erforderliche
nicht ausgeführte Prüfungen blockieren weiterhin eine vollständige Produktionsfreigabe.

Den gesamten ausgelieferten Worker und das Runtime-Paket aus eingefrorenen
Eingaben reproduzierbar neu bauen und genau die ausgelieferten Binärdateien
nativ prüfen. Beschaffung aus dem Netz und Kompilierung aus vorhandenen
verifizierten Eingaben getrennt bewerten.

Funktionale Pilotfähigkeit, Geschwindigkeitsziel und verbleibende
Produktionslücken getrennt abnehmen. Das Gesamtziel erst als erreicht melden,
wenn alle vereinbarten Teilziele nachgewiesen sind. Kein VM-Isolationsversprechen,
keine pauschale Kubernetes-/Plattformfreigabe und keine unbewiesene Latenzgarantie.

AGENTS.md bleibt verbindlich: ein Implementierungsautor, keine Subagents,
keine Änderungen an Factory oder historischem Experiment, unveränderte
Referenzdateien/Import-Overlays, geordnete nachvollziehbare Patches,
3 CPU/3 GiB pro Sandbox-Worker, separate Theia mit 1 CPU/768 MiB,
höchstens zwei lebende Sandboxes und 420 Sekunden je vollständigem
Lifecycle-Lauf. Neue Fälle dürfen eigene begrenzte Gates erhalten.
Native Läufe und Builds erhalten vorher eigene Artefakte und halten das
Speicherbudget sowie die Regeln für exakte Eigentumsprüfung und Bereinigung ein.
Keine Host-Runtime-Installation, globalen sysctls, KVM, neuen VMs oder Veröffentlichung.
