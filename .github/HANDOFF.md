# Plex Media Manager – Entwicklungs- und Betriebshandoff

Stand: 2026-11-27, letzter Live-Check 00:25 Uhr (lokale Zeit)

Dieses Dokument ist die operative Übergabe für die nächste Arbeitssitzung.
Vor Neustarts oder Änderungen den dann aktuellen Queue- und NAS-Zustand erneut
prüfen. Geheimnisse und Kennwörter gehören weder hierher noch in Logs.

## Laufender Stand

- Repository: `C:\PlexMediaManager\tools\m3u-downloader`
- Windows-Host: `GOG-FOTOBOX`, Benutzerkonto `Mediacenter`
- Weboberfläche: `http://127.0.0.1:8765` lokal, im LAN über
  `http://<PC-IP>:8765`
- `/api/health`: `{"database":"connected","status":"ok"}`
- Letzter Dashboard-Check:
  - Katalog: 62 Kategorien, 27.226 Filme, 5.348 Serien
  - Aufträge insgesamt: 1 aktiv, 49 wartend, 107 abgeschlossen, 0 fehlgeschlagen,
    0 abgebrochen
  - Aktiv ist Job 1407: *Chicago Fire*, S03E19, Downloadphase
    (ca. 64 MB von 529 MB)
- Für Serie 482 (*How I Met Your Mother*) lieferte `/api/jobs` beim letzten Check
  keine Jobzeilen. Nicht annehmen, dass alte Queue- oder Testdaten noch bestehen;
  vor erneutem Einreihen API und NAS-Dateien abgleichen.
- Eine Pausenregel ist gespeichert, aber Downloads und IPTV waren nicht pausiert.
- Die Windows-Aufgaben `PlexMediaManager-Web` und `PlexMediaManager-Worker`
  liefen mit dem am 2026-11-27 um ca. 00:20 gebauten Binary.
- NAS-Laufwerke `Z:` (Filme) und `Y:` (Serien) sowie lokale Temp-Ordner waren
  beim letzten Check vorhanden.
- Job 1407 wurde vor dem Deployment kontrolliert pausiert und danach mit der
  Queue wieder aufgenommen. Vor dem Neustart waren ca. 404 MB gespeichert;
  nach dem Neustart begann die lokale `.raw.part` wieder nahe 0. Ob der Provider
  Range-Requests ignoriert oder die vorherige Teil-Datei anderweitig verloren
  ging, ist ungeklärt. Den aktiven Job nicht nur zur Diagnose neu starten.

## Bereits umgesetzt

- Go-Webserver mit eingebettetem Dashboard, SQLite-Katalog und Queue.
- Serien-, Staffel- und Episodenjobs, persistente Queue-Reihenfolge und
  Fortschritts-/Phasenfelder.
- Filmjobs sowie Serien-, Staffel- und Episodengruppen lassen sich über eigene
  Griffe sortieren. Episoden werden innerhalb ihrer Staffel, Staffeln innerhalb
  ihrer Serie verschoben; die Eltern-/Kind-Reihenfolge bleibt erhalten. Nur
  der Griff fängt Touch-Ziehen ab, der übrige Bildschirm bleibt scrollbar.
- Queue-Aktualisierungen ignorieren veraltete, parallel laufende Antworten
  während Drag-and-drop und verwenden für die Queue-Abfrage `no-store`.
  Staffelreihenfolge wurde im laufenden Dashboard geändert und blieb auch nach
  dem automatischen Fünf-Sekunden-Refresh bestehen; die ursprüngliche Reihenfolge
  wurde anschließend wiederhergestellt.
- Filme und einzelne Folgen zeigen bei abgeschlossenem Download
  „Bereits heruntergeladen“; Staffeln zeigen es, wenn alle Episoden
  abgeschlossen sind. Diese Buttons sind grün und deaktiviert. Der Status
  basiert auf abgeschlossenen Queue-Jobs.
- Die Queue-, Katalog-, Pause- und Dashboard-Layouts umbrechen längere Texte
  mobil und vermeiden horizontales Überlaufen.
- Jobphasen: `queued`, `download`, `remux`, `transfer`, `completed`.
  Fortschritt und ETA werden aus Jobdaten in der Oberfläche berechnet.
- Download und Remux erfolgen lokal in den konfigurierten Temp-Ordnern. Erst
  fertig verarbeitete Dateien werden in die NAS-Bibliothek übertragen.
- SMB-Watchdog prüft Film- und Serienziel getrennt, verbindet Shares bei Bedarf
  erneut und lässt fertige lokale `.processing`-Dateien bis zur NAS-Rückkehr
  bestehen.
- Beim Workerstart werden unterbrochene `active`-Jobs zu `pending` zurückgesetzt,
  ohne Retry-Versuch zu verbrauchen oder die Download-Teilbytes zu löschen.
  HTTP-Range-Downloads können dadurch vorhandene `.part`-Dateien fortsetzen.
  Ein Neustart der Weboberfläche ist für diese Wiederherstellung nicht nötig.
- Die Worker-Pipeline hält Provider-Mediendownloads einzeln und startet den
  nächsten etwa fünf Sekunden nach Eintritt des vorherigen Jobs in Remux bzw.
  Transfer. Remux und NAS-Transfer früherer Jobs laufen dabei parallel weiter.
- Pausen & Zeitpläne: persistente Regeln für `downloads`, `iptv` oder `both`;
  dauerhaft, Dauer (1 Minute bis 7 Tage), Startzeit oder Wochenzeitplan.
  IPTV-Aufrufe werden am Xtream-Client-Gate angehalten; Downloads können aktiv
  unterbrochen und später fortgesetzt werden.
- SQLite-Schema und Migrationen sind serialisiert. Regressionstest für
  parallele Legacy-Datenbankstarts vorhanden.
- NAS-Zugangsdaten liegen außerhalb der TOML-Datei in den Windows-
  Anmeldeinformationen/Credential Manager.

## Wichtige Pfade

### Projekt und Build

- Go-Einstieg und Prozessverdrahtung:
  `cmd\m3u-dl\main.go`
- Modul-/Abhängigkeitsdefinition: `go.mod`
- Laufender Übergabestand: `.github\HANDOFF.md`
- Sitzungsverlauf mit den zuletzt vorgemerkten Feature-Wünschen:
  `.github\lastSession.md`
- Windows-Binary, das die geplanten Tasks verwenden:
  `C:\PlexMediaManager\tools\m3u-downloader\m3u-dl.exe`
- Dashboard-HTML/CSS/JavaScript (wird vom Go-Server eingebettet):
  `internal\web\frontend\index.html`
- Windows-Tasksteuerung:
  `C:\PlexMediaManager\tools\PlexMediaManager-ServiceControl.ps1`
- Desktopverknüpfung:
  `C:\Users\Mediacenter\Desktop\Plex Media Manager Steuerung.lnk`

### Konfiguration und Zustand

- Konfiguration:
  `%APPDATA%\m3u-dl\config.toml`
  (derzeit `C:\Users\Mediacenter\AppData\Roaming\m3u-dl\config.toml`)
- SQLite-Datenbank:
  `%LOCALAPPDATA%\m3u-dl\state.db`
  (derzeit `C:\Users\Mediacenter\AppData\Local\m3u-dl\state.db`)
- Workerlog:
  `%LOCALAPPDATA%\m3u-dl\m3u-dl.log`
- Beispielkonfiguration:
  `config.example.toml`

### Konfigurierte Medienpfade (letzter geprüfter Stand)

- Temp-Filme (`[output].movies_dir`):
  `C:\PlexMediaManager\temp\IPTV\Movies`
- Temp-Serien (`[output].series_dir`):
  `C:\PlexMediaManager\temp\IPTV\Series`
- NAS-Filmfreigabe: `\\192.168.178.114\video\Movies`, eingebunden als `Z:`
- NAS-Serienfreigabe: `\\192.168.178.114\video\Series`, eingebunden als `Y:`
- `nas.enabled = true`, Watchdog-Prüfintervall 30 Sekunden,
  Reconnect-Intervall 60 Sekunden.
- Für Änderungen an den Sharepfaden erst `config.toml`, Windows-Mappings und
  Verfügbarkeit der exakten Zielordner zusammen prüfen. Keine Kennwörter
  dokumentieren.

## Relevante Implementierungsdateien

- `internal\config\config.go` – Konfigurationspfade, TOML und NAS-Parameter
- `internal\nas\watchdog.go` – SMB-Zustandsprüfung und Reconnect
- `internal\worker\worker.go` – Download, Pause, Remux, NAS-Transfer und
  Wiederherstellung unterbrochener Jobs
- `internal\downloader\downloader.go` – HTTP-Range/Fortsetzung von `.part`
- `internal\store\schema.sql` – Tabellen und Indizes
- `internal\store\store.go` – Schemaaufbau und serialisierte Migrationen
- `internal\store\jobs.go` – Queue-Zustand, Reihenfolge, Phasen und Recovery
- `internal\pause\controller.go` – Pausenvalidierung und Zeitplanberechnung
- `internal\web\server.go` – HTTP-Routen und Request-Handler
- `internal\catalog\enqueue.go` – Film-/Serien-/Staffel-/Episodenjobs
- `internal\worker\worker_test.go`, `internal\store\jobs_test.go`,
  `internal\store\migration_test.go`, `internal\web\server_test.go` –
  passende Regressionstests

## HTTP API

Basisadresse lokal: `http://127.0.0.1:8765`. API-Antworten sind JSON.
Fehlerantworten verwenden HTTP-Fehlerstatus; doppelte Queue-Einträge liefern
`409 Conflict`.

### Status, Katalog und Details

| Methode und Pfad | Parameter / Zweck |
|---|---|
| `GET /api/health` | Erreichbarkeit von Server und Datenbank |
| `GET /api/dashboard` | Katalog- und Jobzähler |
| `GET /api/categories?type=vod\|series&nsfw=0\|1` | Kategorien |
| `GET /api/catalog?type=vod\|series` | Zusätzlich `page`, `limit` (max. 100), `q`, `category_id`, `nsfw`, `sort`; Antwort enthält `items`, `total`, `page`, `limit`, `totalPages` |
| `GET /api/movie?id=<id>` | Film-Metadaten |
| `GET /api/cover/movie?id=<id>` | Serverseitiger Poster-Proxy |
| `GET /api/series?id=<id>` | Serie und Staffeln; kann Seriendetails beim Provider nachladen |
| `GET /api/episodes?series_id=<id>&season=<n>` | Episoden einer Staffel |
| `GET /api/jobs?status=pending,active` | Queue; `status` optional, zulässige Werte `pending`, `active`, `completed`, `failed`, `cancelled` |

`/api/jobs`-Elemente enthalten unter anderem `id`, `kind`, `source_id`,
`series_id`, `season`, `episode_number`, `status`, `phase`, `progress_bytes`,
`total_bytes`, `attempts`, `last_error`, `queue_position`, `started_at` und
`phase_started_at`.

### Queue-Einträge und Queue-Aktionen

| Methode und Pfad | Parameter / Request-Body |
|---|---|
| `GET /api/queue/movie?id=<id>` | Film-Queue-Status einschließlich `downloaded` |
| `POST /api/queue/movie` | `{"id": <movie_id>}` |
| `DELETE /api/queue/movie?id=<id>` | Film aus wartender/laufender Queue entfernen |
| `GET /api/queue/episode?id=<id>` | Episoden-Queue-Status einschließlich `downloaded` |
| `POST /api/queue/episode` | `{"series_id": <id>, "episode_id": <id>}` |
| `DELETE /api/queue/episode?id=<id>` | Episode entfernen |
| `GET /api/queue/season?series_id=<id>&season=<n>` | Staffelstatus, Anzahl und `downloaded` (nur wenn alle Folgen abgeschlossen sind) |
| `POST /api/queue/season` | `{"series_id": <id>, "season": <n>}` |
| `DELETE /api/queue/season?series_id=<id>&season=<n>` | Staffeljobs entfernen |
| `GET /api/queue/series?id=<id>` | Serienstatus und Anzahl |
| `POST /api/queue/series` | `{"series_id": <id>}`; reiht Serie ein und abonniert künftige Folgen |
| `DELETE /api/queue/series?id=<id>` | Wartende/laufende Episodenjobs der Serie entfernen |
| `POST /api/queue/reorder` | `{"job_ids": [<id>, ...]}`; persistente Reihenfolge wartender Jobs |
| `POST /api/jobs/{id}/retry` | Fehlgeschlagenen/abgebrochenen Job erneut einreihen |
| `POST /api/jobs/{id}/cancel` | Laufenden oder wartenden Job abbrechen |
| `DELETE /api/jobs/{id}` | Jobdatensatz entfernen |

### Pausen

| Methode und Pfad | Zweck |
|---|---|
| `GET /api/pauses` | Gibt `rules` und den Zustand `paused_downloads`/`paused_iptv` zurück |
| `POST /api/pauses` | Pausenregel anlegen; Antwort `201` |
| `DELETE /api/pauses/{id}` | Regel löschen und die betroffenen Bereiche freigeben, sofern keine weitere Regel aktiv ist |

POST-Body-Felder:

- Immer: `scope` (`downloads`, `iptv`, `both`), `mode`
- `mode=indefinite`: sofort und bis zum Löschen
- `mode=duration`: `duration_minutes` von 1 bis 10080
- `mode=at`: `start_at` als RFC3339; optional `end_at` als RFC3339
- `mode=schedule`: `days` (Sonntag `0` bis Samstag `6`), `start_time` und
  `end_time` als `HH:MM`; Übernacht-Zeiträume werden unterstützt

Beispiel:

```powershell
$body = @{
    scope = "downloads"
    mode = "duration"
    duration_minutes = 30
} | ConvertTo-Json
Invoke-RestMethod -Uri "http://127.0.0.1:8765/api/pauses" `
    -Method Post -ContentType "application/json" -Body $body
```

## Geplante Windows-Aufgaben und Betrieb

- Task Scheduler-Aufgaben: `PlexMediaManager-Web` und
  `PlexMediaManager-Worker`
- Beide Tasks starten:
  `C:\PlexMediaManager\tools\m3u-downloader\m3u-dl.exe`
- Argumente: `web` beziehungsweise `worker`
- Webbindung: `0.0.0.0:8765`; Firewallregel:
  `PlexMediaManager-Web-8765` (TCP 8765)
- Beide Tasks über den vorhandenen Neustartknopf im Steuerungsfenster oder aus
  PowerShell neu starten:

```powershell
schtasks.exe /End /TN "PlexMediaManager-Worker"
schtasks.exe /End /TN "PlexMediaManager-Web"
Start-Sleep -Seconds 2
schtasks.exe /Run /TN "PlexMediaManager-Worker"
schtasks.exe /Run /TN "PlexMediaManager-Web"
```

Das Steuerungsfenster verwendet `Get-ScheduledTask`, startet bereits laufende
Tasks nicht erneut und wartet bis zu 15 Sekunden auf den erwarteten Taskstatus.
Fehler von `schtasks.exe` zeigen Kommando, Exitcode und Prozessausgabe an.
Der Fehler im Steuerungsfenster wurde behoben: `schtasks.exe` wird über
`System.Diagnostics.ProcessStartInfo` statt mit widersprüchlichen
`Start-Process`-Parametern aufgerufen. Die direkte Task-Ausführung wurde
verifiziert; beide Tasks und die Health-API liefen danach. Ein echter
Neustart über den GUI-Knopf wurde wegen des aktiven Downloads noch nicht
ausgelöst. Nach einer Änderung am Skript das Steuerungsfenster schließen und
neu öffnen, damit es den aktualisierten Code lädt.

Bei einem Worker-Neustart bleibt die Queue in SQLite erhalten. Ein gerade
aktiver Download wird bei erneutem Workerstart automatisch wieder eingereiht;
den tatsächlichen Fortschritt anschließend über `GET /api/jobs` und bei Bedarf
die lokale `.raw.part`-Datei prüfen. Bei Job 1407 wurde die Datei nach dem
kontrollierten Pause-/Neustart nahe 0 neu angelegt, obwohl zuvor ca. 404 MB
gespeichert waren. Range-Unterstützung des Providers und Logs prüfen, wenn das
ohne Unterbrechung des laufenden Jobs möglich ist.

## Prüfen und bauen

Im Projektordner:

```powershell
go test ./...
go vet ./...
go build -o .\m3u-dl.exe .\cmd\m3u-dl
```

Der Worker pollt eine leere Queue alle 250 ms. Die aktuell laufenden Web- und
Worker-Tasks verwenden das am 2026-11-27 um ca. 00:20 gebaute Binary. Nach
künftigen Quellcodeänderungen erneut bauen und beide Tasks kontrolliert
neustarten. Vor Neustarts aktive Jobs, Queue und Teil-Dateien prüfen.
Zwischen dem Abschluss eines
Providerdownloads und dem Start des nächsten liegt ein Mindestabstand von
ungefähr fünf Sekunden; Remux und Transfer können sich mit Folgedownloads
überlappen.

## Noch offene, vom Nutzer vorgemerkte Arbeiten

1. In der Film- und Serienübersicht eine alphabetische Schnellnavigation am
   unteren Rand, ähnlich der Pagination, ergänzen: zuerst Zahlen, danach A–Z;
   Buchstaben anklickbar, kein Dropdown. Auswahl soll auf Einträge mit diesem
   Anfangsbuchstaben springen/filtern.

Die Queue-Gruppenverschiebung, mobile Textumbrüche und Download-Pipeline wurden
im Quellcode implementiert und getestet. Die alphabetische Schnellnavigation ist
die noch offene Funktion.
