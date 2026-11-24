# Plex Media Manager - Übergabestand

Stand: 2026-11-24

Diese Datei dient als aktuelle Entwicklungs- und Betriebsübergabe für die nächste
Session. Quelle und bestehende Datenbank dürfen erweitert, aber nicht ersetzt werden.

## Repository und Ziel

- Repository: `C:\PlexMediaManager\tools\m3u-downloader`
- Technik: Go, SQLite, Xtream Codes, eingebettetes Browser-Frontend
- Aktuelles Zielsystem: Windows-PC `GOG-FOTOBOX`
- Geplantes Zielsystem: Windows-PC mit Synology DS1525+ und Plex
- Datenbank: `%LOCALAPPDATA%\m3u-dl\state.db`
- Konfiguration: `%APPDATA%\m3u-dl\config.toml`

## Aktuell bestätigter Entwicklungsstand

### Backend und Daten

- Go-Anwendung baut erfolgreich.
- SQLite-Datenbank ist verbunden und verwendet die bestehende `state.db`.
- Synchronisierte Daten:
  - 62 Kategorien
  - 27.226 VOD-Filme
  - 5.348 Serien
- Katalog, Seriendetails, Staffeln und Episoden werden aus SQLite bedient.
- Bestehender Downloadworker und bestehende Queue wurden erhalten.
- Downloadjobs unterstützen `pending`, `active`, `completed`, `failed`, `cancelled`.
- Episodenjobs speichern jetzt zusätzlich:
  - `series_id`
  - `season_number`
  - `episode_number`

### API

- `GET /api/health`
- `GET /api/dashboard`
- `GET /api/categories`
- `GET /api/catalog`
- `GET /api/movie`
- `GET /api/cover/movie?id=<id>`
- `GET /api/series`
- `GET /api/episodes`
- `GET /api/jobs`
- `GET /api/queue/movie`
- `POST /api/queue/movie`
- `DELETE /api/queue/movie`
- `GET /api/queue/episode`
- `POST /api/queue/episode`
- `DELETE /api/queue/episode`
- `GET /api/queue/season`
- `POST /api/queue/season`
- `DELETE /api/queue/season`
- `GET /api/queue/series`
- `POST /api/queue/series`
- `DELETE /api/queue/series`
- `POST /api/jobs/{id}/retry`
- `POST /api/jobs/{id}/cancel`
- `DELETE /api/jobs/{id}`

### Frontend

- Dashboard
- Filmübersicht und Serienübersicht
- Suche, Kategorieauswahl und Pagination
- Sortierung nach Name, Jahr und Bewertung
- NSFW-Trichterfilter mit `localStorage`-Persistenz
- Standardmäßig werden Kategorien und Filme mit Namen `FOR ADULTS%` ausgeblendet
- Film- und Serien-Detailansichten
- Staffeln und Episoden in Serienansicht
- Downloadbutton pro Film in Detailansicht
- Hover-Downloadbutton auf Filmcovern
- Downloadbutton pro Episode
- Downloadbutton für gesamte Staffel
- Downloadbutton für gesamte Serie
- Gruppierte Warteschlange:
  - Serie als Hauptobjekt
  - Staffeln eingerückt
  - Episoden als kompakte Zeilen
  - Aufklappen über Pfeil bleibt über periodische Aktualisierung erhalten
- Filmjobs rechtsbündige Queue-Aktionen
- Staffel-/Serienentfernen löscht alle passenden Jobzeilen
- Abgebrochene und fehlgeschlagene Jobs können einzeln erneut eingereiht werden
- Cover werden über `GET /api/cover/movie` serverseitig geladen
- Fehlende oder ungültige Cover verwenden weiterhin einen Frontend-Platzhalter

### Cover-Proxy

- Externe `http`/`https`-Poster werden vom Go-Backend geladen.
- Antwort muss ein Bild sein.
- Timeout: 15 Sekunden
- Antwortlimit: 20 MB
- Cache-Control: 24 Stunden
- Liveprüfung bestätigt `image/jpeg` mit `200 OK` für ein vorhandenes Cover.
- Fehlende und unbekannte Cover liefern `404`.

### Windows-Dienste

Am 2026-11-23 wurden zwei Windows-geplante Aufgaben eingerichtet:

- `PlexMediaManager-Worker`
- `PlexMediaManager-Web`

Beide Aufgaben:

- wurden für Benutzer `Mediacenter` registriert
- starten beim Anmelden
- werden nach Anmeldung des Benutzers ausgeführt
- wurden live als aktiv gestartet geprüft
- Webbindung: `0.0.0.0:8765`
- Weboberfläche: `http://<Windows-PC-IP>:8765`
- Firewallregel: `PlexMediaManager-Web-8765`, TCP-Port 8765

Wichtige Implementierungsdateien:

- `internal/service/service_other.go`
- `internal/config/config.go`
- `config.example.toml`

Desktopsteuerung:

- Datei: `C:\PlexMediaManager\tools\PlexMediaManager-ServiceControl.ps1`
- Verknüpfung:
  `C:\Users\Mediacenter\Desktop\Plex Media Manager Steuerung.lnk`
- Aktionen:
  - Start
  - Stop
  - Neustart
  - Weboberfläche öffnen

Das Skript verwendet `schtasks.exe` für die beiden geplanten Tasks.

## Verifikation

Folgendes wurde bestätigt:

- `go test ./...` erfolgreich
- `go build ./cmd/m3u-dl` erfolgreich
- Windows-Cross-Build erfolgreich
- `/api/health` liefert `{"database":"connected","status":"ok"}`
- `/api/dashboard` liefert die Live-Katalog- und Jobzahlen
- Der laufende Server antwortet auf Port 8765
- LAN-Binding war über `::` und `127.0.0.1:8765` erreichbar
- Cover-Proxy wurde live geprüft
- Dashboard zeigt Livewerte statt Dauerstatus
- Series-/Staffel-/Episodequeue-Aktionen wurden per Store- und HTTP-Test geprüft

## Bekannte Betriebsgrenzen

- Die geplanten Tasks laufen für Benutzer `Mediacenter` im interaktiven Modus.
- Eine Dienstanmeldung ohne Benutzeranmeldung wird derzeit nicht benötigt.
- Für Task-Erstellung oder Änderungen sind Administratorrechte nötig.
- Die Worker-Ausgabe verwendet den vorhandenen Worker, nicht einen separaten Prozess.
- NAS-Mount-Wiederverbindung ist noch nicht implementiert.
- Der Worker markiert einen ungemounteten Zielpfad momentan als permanenten Fehler.
- Keine dauerhafte Credentials-Speicherung im NAS-Mount-Modul ist implementiert.
- Die Zielpfade sind noch lokale Windows-Pfade und müssen beim NAS-Umzug geprüft werden.

## Aktueller Entwicklungsstop

Die Zwischensicherung soll als Zeitpunkt vor dem NAS-Mount-Projekt dienen.

Umgesetzt sind:

- Browserworkflow
- Queueverwaltung
- Film-/Serien-/Staffel-/Episodeaktionen
- NSFW-Filter
- Cover-Proxy
- Windows-Dienste
- Desktopsteuerung

Noch nicht umgesetzt:

- Dauerhafte SMB/NAS-Mountverwaltung
- Automatische Reconnects nach Netzwerk- oder NAS-Trennung
- Worker-Pause bei nicht erreichbarem NAS
- Wiederaufnahme wartender Jobs nach Mount-Reconnect
- Plex-Refresh nach vollständiger Download-Finalisierung

## Nächster Entwicklungsschritt

Implementiere einen Windows-NAS-Mount-Watchdog.

Anforderungen:

1. Eine SMB-Verbindung zum Synology-NAS wird dauerhaft geprüft.
2. Der Worker läuft weiter, erkennt aber eine nicht erreichbare Bibliothek.
3. Beim Verbindungsverlust wird kein aktiver Job als permanent fehlgeschlagen markiert.
4. Der Mount wird periodisch neu aufgebaut.
5. Nach erfolgreicher Rückkehr werden wartende Jobs normal verarbeitet.
6. Beide Ausgabeordner werden getrennt geprüft:
   - Filmziel
   - Serienziel
7. Mount- und Reconnect-Informationen werden sichtbar protokolliert.
8. Keine Passwortwerte werden in Logs oder Konfigurationsdokumentation übernommen.

Der Watchdog soll inkrementell in den bestehenden Worker integriert werden.
Er soll keine neue Queue, keine zweite Datenbank und keinen Ersatzworker erzeugen.
