# VanillaNUT

Dashboard monitorowania UPS przez protokół NUT (Network UPS Tools), napisany w Go.

---

## Jak działa

VanillaNUT łączy się z jednym lub wieloma serwerami NUT (Network UPS Tools) i wyświetla stan podłączonych zasilaczy UPS w przeglądarce jako czytelny dashboard.

Przy starcie aplikacja odpytuje wszystkie skonfigurowane serwery NUT równolegle i zapisuje wynik w pamięci podręcznej. Następnie cyklicznie (co `REFRESH_INTERVAL` sekund) odświeża dane w tle — dzięki temu strona ładuje się natychmiast, bez czekania na odpowiedź serwerów NUT. Przeglądarka automatycznie przeładowuje stronę po upływie tego samego interwału.

Dla każdego UPS wyświetlane są:
- status (Online / Zasilanie bateryjne / Ładowanie)
- poziom naładowania baterii
- aktualne zużycie mocy vs. moc nominalna
- szacowany czas pracy na baterii
- napięcie baterii, wejścia i wyjścia

Aplikacja działa jako pojedyncza, samodzielna binarka — wszystkie zasoby (Bootstrap, ikony, fonty, szablony HTML) są wbudowane w plik wykonywalny. Nie wymaga Dockera, serwera WWW ani połączenia z internetem.

---

## How it works

VanillaNUT connects to one or more NUT (Network UPS Tools) servers and presents the status of connected UPS devices as a web dashboard.

On startup, the application queries all configured NUT servers in parallel and stores the result in an in-memory cache. It then refreshes the cache in the background at a configurable interval (`REFRESH_INTERVAL` seconds) — so the page loads instantly without waiting for NUT responses. The browser automatically reloads after the same interval.

For each UPS the dashboard shows:
- status (Online / On battery / Charging)
- battery charge level
- current power draw vs. nominal power
- estimated battery runtime
- battery, input and output voltage

The application ships as a single self-contained binary — all assets (Bootstrap, icons, fonts, HTML templates) are embedded at compile time. No Docker, no web server, no internet connection required.

---

## Wymagania / Requirements

- Go 1.22+ (only for compilation)
- Network access to NUT servers (default port 3493)

---

## Konfiguracja / Configuration

```bash
cp .env.example .env
```

```env
PORT=5000
NUT_SERVERS=192.168.1.10:3493,192.168.1.11:3493
NUT_MODEL_NAMES=VP1200ELCD:UPS Mac,VP1600ELCD:UPS Server
NUT_OL_CHRG_AS_ONLINE=VI2200SHL
REFRESH_INTERVAL=30
```

| Variable                | Description                                                                                   |
|-------------------------|-----------------------------------------------------------------------------------------------|
| `PORT`                  | HTTP listen port (default: `5000`)                                                            |
| `NUT_SERVERS`           | Comma-separated list of NUT servers, format `host:port`                                       |
| `NUT_MODEL_NAMES`       | Model-to-friendly-name mappings, format `MODEL:Name,MODEL2:Name2`                             |
| `NUT_OL_CHRG_AS_ONLINE` | Models for which `OL CHRG` status is shown as "Online" instead of "Charging" (comma-separated) |
| `REFRESH_INTERVAL`      | Data and page refresh interval in seconds (default: `30`)                                     |

---

## Uruchomienie / Running

```bash
# Without compilation
/usr/local/go/bin/go run .

# Build and run
go build -o vanillanut .
./vanillanut
```

---

## Uruchomienie jako usługa systemd / Running as a systemd service

### 1. Kompilacja / Build

```bash
go build -o vanillanut .
```

### 2. Instalacja / Install

```bash
sudo mkdir -p /opt/vanillanut
sudo cp vanillanut /opt/vanillanut/
sudo cp .env /opt/vanillanut/
```

### 3. Plik usługi / Service file

```bash
sudo nano /etc/systemd/system/vanillanut.service
```

```ini
[Unit]
Description=VanillaNUT - UPS monitoring dashboard
After=network.target

[Service]
Type=simple
User=kamil
WorkingDirectory=/opt/vanillanut
EnvironmentFile=/opt/vanillanut/.env
ExecStart=/opt/vanillanut/vanillanut
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

#### Zmienne bezpośrednio w pliku usługi / Variables directly in the service file

Zamiast trzymać konfigurację w osobnym pliku `.env`, można umieścić zmienne wprost w pliku usługi za pomocą dyrektyw `Environment=`. Wtedy pomija się `EnvironmentFile=` i krok kopiowania `.env`.

Instead of keeping configuration in a separate `.env` file, you can place the variables directly in the service file using `Environment=` directives. In that case skip `EnvironmentFile=` and the step of copying `.env`.

```ini
[Unit]
Description=VanillaNUT - UPS monitoring dashboard
After=network.target

[Service]
Type=simple
User=kamil
WorkingDirectory=/opt/vanillanut
Environment=PORT=5000
Environment=NUT_SERVERS=192.168.1.10:3493,192.168.1.11:3493
Environment=NUT_MODEL_NAMES=VP1200ELCD:UPS Mac,VP1600ELCD:UPS Server
Environment=NUT_OL_CHRG_AS_ONLINE=VI2200SHL
Environment=REFRESH_INTERVAL=30
ExecStart=/opt/vanillanut/vanillanut
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Uwagi / Notes:

- Każda zmienna to osobna linia `Environment=`. Wartości ze spacjami (np. `NUT_MODEL_NAMES`) nie wymagają cudzysłowów, ale jeśli ich użyjesz, cała para musi być w jednym cudzysłowie: `Environment="NUT_MODEL_NAMES=VP1200ELCD:UPS Mac"`.
- Po każdej zmianie pliku usługi wykonaj `sudo systemctl daemon-reload` i `sudo systemctl restart vanillanut`.
- Plik usługi jest czytelny dla wszystkich (`/etc/systemd/system`). Jeśli konfiguracja zawiera dane wrażliwe, pozostań przy `EnvironmentFile=` z ograniczonymi uprawnieniami (`sudo chmod 600 /opt/vanillanut/.env`).

- Each variable is a separate `Environment=` line. Values with spaces (e.g. `NUT_MODEL_NAMES`) don't need quotes, but if you use them, the whole pair must be quoted: `Environment="NUT_MODEL_NAMES=VP1200ELCD:UPS Mac"`.
- After any change to the service file run `sudo systemctl daemon-reload` and `sudo systemctl restart vanillanut`.
- The service file is world-readable (`/etc/systemd/system`). If your configuration contains sensitive data, keep using `EnvironmentFile=` with restricted permissions (`sudo chmod 600 /opt/vanillanut/.env`).

### 4. Uruchomienie / Start

```bash
sudo systemctl daemon-reload
sudo systemctl enable vanillanut
sudo systemctl start vanillanut
sudo systemctl status vanillanut
```

### Przydatne komendy / Useful commands

```bash
sudo systemctl stop vanillanut
sudo systemctl restart vanillanut
sudo journalctl -u vanillanut -f
```

---

## Aktualizacja / Update

```bash
go build -o vanillanut .
sudo cp vanillanut /opt/vanillanut/
sudo systemctl restart vanillanut
```
