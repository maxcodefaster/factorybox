# Factorybox in die Open Software Factory integrieren

Planungsstand: 25. Oktober 2026. Erster Consumer ist das private Repository
`maxcodefaster/open-software-factory`, geprüft auf Commit
`0e2eeabca446a19b9ea533f58a3f2d6864d4332f`. Die Prüfung liest Quellcode und
Charts; sie ist kein Test einer laufenden Installation. Der erste Worker-Anschluss
ist inzwischen in beiden Repositories umgesetzt und nativ geprüft. Der genaue
[Umsetzungsstand](kubernetes-worker.md) trennt diesen administrativen Anschluss
von der noch offenen Umschaltung der normalen Factory-Workspaces.

Dieser Plan ersetzt den vorherigen allgemeinen Vorschlag für eine neue
Poolsteuerung. Die Factory besitzt bereits eine Kubernetes-Anbindung und eine
Reserveverwaltung. Eine zweite Steuerung samt zusätzlicher Claim-CRD ist für
dieses erste Ziel nicht vorgesehen. Der bestehende Umfang von `GOAL.md` und der
[Freigabestand](first-release-status.md) bleiben erhalten; das blockierte Ziel
wird durch diese Planung nicht fortgesetzt.

## Was bereits vorhanden ist

| Bestandteil | Tatsächlicher Repository-Stand |
| --- | --- |
| Installation | Helm-Chart für vorhandene Kubernetes-Cluster; interne Services, ServiceAccount und konfigurierbare StorageClass |
| Ausführung | Kubernetes-Pods mit Envbox, darin Sysbox, privates Docker, Dev Containers und der Factory Runtime Connector |
| Bereitschaft und Zugriff | Prüfung exakter Ressourcen/Images, Connector-Verbindungen und getrennte Browser-/IDE-Zugänge |
| Reserveverwaltung | Dauerhafte PostgreSQL-Slots, begrenztes Auffüllen, exakte Vergabe und Schutz vor Wiederverwendung benutzter Slots |
| Reservegrenzen | Aktuelle Rollen sind Staging und Verification; das Helm-Chart setzt `FACTORY_EXECUTION_RESERVE_TARGET=0` |
| Projektdaten | Eigener dauerhafter Workspace-Speicher und unabhängiger Dateidienst; der Editor funktioniert auch ohne laufende Ausführung |

Die bestehende Reserve ist kein Nachweis eines vollständigen, bereits mit
einem persönlichen Projekt und OpenCode geladenen Workspace-Pools.
Persönliche und Ticket-Workspaces behalten ihre eigenen Quellen und Daten.
Die reservierten Slots dürfen nicht durch einen PVC-Tausch oder die
Neuzuordnung benutzter Boxen zu persönlichen Workspaces gemacht werden.

## Was sich konkret ändern muss

Erledigt sind der dedizierte Worker-Pod mit dauerhaftem vollständigem Store,
HTTP-Bereitschaft ohne frühe Exec-Prozesse, Prüfung von Node-/Speicher-/Worker-
Identitäten und epochengenau gebundene CLI-Aufrufe. Ein vollständiger nativer
Adapterlauf bestätigt Start, erste Arbeit, RAM-freigebende Pause, Fortsetzen
mit unverändertem RAM-/Dateizustand, exakte Wiederholung und Löschen.
Die Kubernetes-Beobachtungen dieses Laufs sind Fixtures. Ein echtes Cluster
war hier nicht verbunden. Die bestehende Reserve- und Workspace-Steuerung ist
noch nicht auf Factorybox umgeschaltet.

1. Die bestehende Runtime-Anbindung muss den Factorybox-Worker und dessen
   CLI für Start, Pause, Fortsetzen und begrenzte Recovery ansprechen.
   Zustände, exakte Anforderungsidentität, Epoche und Zugriffsrechte müssen
   in die vorhandenen Runtime Attempts und Connector-Verbindungen passen.
   Ein neues Image allein ändert diese Verträge noch nicht. Die übrige
   Factory-Anwendung und ihre Kubernetes-Installation bleiben die Grundlage.

2. Das Speicherlayout muss zusammengeführt werden. Factorys heutige
   Envbox-Pods speichern ihre Sysbox-Daten in einem `emptyDir`; Factorybox
   braucht seinen vollständigen eigenen persistenten Store.
   Die Factory teilt außerdem kanonische Quell-Inodes zwischen Dateidienst
   und Ausführung. Factorybox hat bisher einen enger geprüften privaten
   Dateisystem-/Mount-Vertrag. Seine vollständige Sicherung kann nicht
   ungeprüft über den bestehenden Workspace-Speicher gelegt werden.
   Insbesondere dürfen Änderungen aus dem unabhängigen Editor während einer
   Pause nicht verloren gehen. Vor dem Implementieren wird eine klare
   Speicherregel festgelegt und mit genau diesem Ablauf geprüft.

3. Die bereits vorhandene Reserveverwaltung wird dort erweitert, wo
   persönliche beziehungsweise Ticket-Workspaces sie sinnvoll nutzen können.
   Rollen, Projektrezept, Speicherbesitz und Ressourcenbudget bleiben
   verbindlich. Frische Vorbereitung und Wiederaufnahme eines bestehenden
   Nutzerzustands sind verschiedene Fälle. Der bestehende PostgreSQL-Abgleich
   bleibt zuständig; eine zweite globale Pool-Datenbank oder CRD wird vermieden.

4. Die Plattformzulassung muss das gewünschte Betriebsmodell abbilden.
   Das aktuelle AMD64-Paket akzeptiert nur seinen exakt eingebauten Kernel.
   Für eine breiter einsetzbare Version brauchen wir geprüfte Linux-Profile
   und tatsächliche Prüfungen der benötigten Fähigkeiten, Architektur,
   cgroups und Speichersemantik. Die heutige Kernelprüfung wird nicht
   ersatzlos entfernt. Das vorhandene Paket bleibt als bisheriger Nachweis
   unverändert; zusätzliche Plattformen erhalten eigene nachvollziehbare
   Qualifikation.

## Was plattformunabhängig hier bedeutet

Das Ziel ist eine gemeinsame Kubernetes-Anbindung und Helm-Konfiguration für
geeignete Linux-Nodes, unabhängig vom Cloudanbieter. Azure/AKS und Kubernetes
auf eigenen Linux-Rechnern sollen denselben Consumer-Vertrag verwenden.
StorageClass, Registry, Zugangsadressen und zulässige Node-Profile sind
Installationswerte; eigene Azure- und lokale Runtime-Implementierungen sind
nicht vorgesehen.

Das ist keine Zusage für jedes Betriebssystem, jede CPU-Architektur, jeden
verwalteten Kubernetes-Modus oder beliebige Dateisysteme. Factory benötigt
bereits heute kompatible Linux-Worker, privilegierte äußere Pods, lesbare
Kernelmodule und passende persistente POSIX-Speicherung. Sysbox selbst hat
ebenfalls dokumentierte Kubernetes-/Node-Voraussetzungen.

Portables Starten auf unterstützten Nodes und das Verschieben eines bereits
gesicherten RAM-Zustands sind getrennte Fähigkeiten. Wiederherstellung nach
Host-Neustart oder auf einem anderen Node bleibt unbewiesen und außerhalb
dieser ersten Integration. Ein verschiebbares PVC allein ändert das nicht.
Ein einzelner Linux-Rechner kann die Factory mit Kubernetes betreiben;
die komplette Factory ohne Kubernetes wäre eine zusätzliche Betriebsart.
Factorybox selbst behält seine unabhängige lokale Laufzeit/CLI.

## Kleine erste Umsetzung und gezielte Abnahme

Zuerst werden beim tatsächlichen Clusterzugriff Kernel, Container-Runtime,
cgroup-Verhalten, Privilegien und StorageClass lesend aufgenommen. Die
Bezeichnung Standardcluster erfordert keine Detailrecherche des Nutzers.
Ein einzelner eigens zugeordneter Worker auf einem vorhandenen Node prüft
danach den konkreten Startpfad. Es gibt keine neue VM, keinen neuen Kernel,
keine Host-Runtime-Installation und keine globalen Sysctl-Änderungen.

Vor einer breiten Pool-Erweiterung werden zwei Anschlussfälle nachgewiesen:
ein echter Factory-Workspace über die vorhandenen Zugänge und eine Pause,
während der sich die kanonischen Quellen durch den unabhängigen Editor
ändern. Ihre Fortsetzung muss die aktuellen Dateien und den zugesagten
Prozesszustand korrekt behandeln. Falls diese Verträge Änderungen im Kern
erfordern, werden sie getrennt vom Kubernetes-/Pool-Adapter umgesetzt.

Die kurze Consumer-Abnahme umfasst anschließend echte Nutzung nach Start,
Pause/Fortsetzung, eindeutige gleichzeitige Vergabe, Abgleich nach
Controller-Neustart und bestätigte begrenzte Worker-Recovery.
Der normale Vergabe-/Auffüllablauf erhält drei vollständige Durchläufe.
Fehlerversuche haben vorab festgelegte Unterbrechungspunkte.
Vorhandene Controller- und Connector-Tests werden passend erweitert.
Neue Plattformen brauchen zusätzlich ihre erforderlichen nativen
Laufzeit-/CRIU-/Integrationsprüfungen. Alte Ergebnisse gelten nur für ihre
tatsächlichen Komponenten und Plattform.

Die 3-CPU/3-GiB-Workergrenze, separate Theia-Grenze von 1 CPU/768 MiB,
420 Sekunden je nativem Lifecycle und maximal zwei gleichzeitig lebende
Test-Sandboxes bleiben erhalten. Der vorhandene Factory-Envbox-Renderer
nutzt andere Ressourcenklassen. Diese werden nicht unbemerkt als neuer
Factorybox-Rahmen übernommen.

Es wird keine vollständige historische Diagnosekampagne wiederholt.
100er-Serie, 24-Stunden-Dauerlauf, automatische Node-Skalierung und zusätzliche
Pool-Controller-Hochverfügbarkeit gehören nicht zu dieser ersten Abnahme.
Unveränderte Assertions und vet bleiben erhalten. Fehlende oder gescheiterte
Pflichtprüfungen bleiben offen. Der Ressourcenvergleich erfasst leeren
Worker, aktive/pausierte Boxen, Cache, Vorbereitung und erste nutzbare Arbeit.

## Performance und Aufwand

Ein laufender Pool verkürzt bei einem passenden frischen Workspace die
Wartezeit nach dem Klick; die Vorbereitung geschieht vorher. Eine persönliche
pausierte Sitzung benötigt weiterhin ihre Wiederherstellung. Ein pausierter
Reserve-Pool verkürzt diesen Restore nicht automatisch.
Die bisherigen [12,5 Sekunden Factory-Resume](first-release-performance.md)
sind keine Messung dieser neuen Consumer-Integration.

Auch Sysbox-Systeme können vorgewärmt werden. Ein direkter Performance-Vorsprung
gegen einen gleich vorbereiteten Sysbox-Workspace ist nicht gemessen.
Der konkrete Gewinn von Factorybox ist die vollständige Zustandsverwaltung mit
RAM-freigebender Pause und begrenzter Recovery, eingebunden in die vorhandene
Factory-Steuerung.

Die vorige Schätzung von drei bis fünf Tagen bezog sich auf eine allgemeine
neue Kubernetes-/Pool-Integration und ist nach diesem Repository-Abgleich
keine belastbare Terminplanung. Vorhandene Steuerung reduziert doppelte
Arbeit. Der Speichervertrag und die breite Plattformzulassung sind echte
offene technische Punkte. Nach dem ersten Worker-/Workspace-Anschluss kann
der verbleibende Aufwand anhand dieser Ergebnisse neu begrenzt werden.

## Nachweise

Der gelesene Snapshot samt SHA-256-Belegen liegt lokal unter
`artifacts/factorybox-osf-integration-review-0e2eeabca446/`.
Die eingefrorenen Quellen dienen nur der Untersuchung.

- [Factory Helm-Installation](https://github.com/maxcodefaster/open-software-factory/blob/0e2eeabca446a19b9ea533f58a3f2d6864d4332f/deploy/helm/factory/README.md)
- [Vorhandene Reserve-Steuerung](https://github.com/maxcodefaster/open-software-factory/blob/0e2eeabca446a19b9ea533f58a3f2d6864d4332f/apps/factory/src/runtime/reserve-controller.ts)
- [Reserve-Rollen und Grenzen](https://github.com/maxcodefaster/open-software-factory/blob/0e2eeabca446a19b9ea533f58a3f2d6864d4332f/packages/runtime-domain/src/reserve.ts)
- [Heutiger Envbox-Pod](https://github.com/maxcodefaster/open-software-factory/blob/0e2eeabca446a19b9ea533f58a3f2d6864d4332f/packages/runtime-kubernetes/src/envbox-pod.ts)
- [Unabhängiger Workspace-Dateizugriff](https://github.com/maxcodefaster/open-software-factory/blob/0e2eeabca446a19b9ea533f58a3f2d6864d4332f/docs/adr/0009-independent-workspace-file-access.md)
- [Sysbox Kubernetes-Voraussetzungen](https://github.com/nestybox/sysbox/blob/master/docs/user-guide/install-k8s.md)
