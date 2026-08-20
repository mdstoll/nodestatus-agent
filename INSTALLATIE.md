# Installeren en testen

Alles hieronder is uitgevoerd en geverifieerd op **DebianG3** (192.168.1.102,
Debian 13 trixie, Intel N100) met de app in de iOS 26.5-simulator.

---

## 1. De agent op een Linux-server

### 1.1 Bouwen (op je Mac)

```bash
cd "Server Info/agent" && make release
```

Levert in `agent/dist/`:

| Bestand | Grootte |
|---|---|
| `serverinfo-agent_0.1.0_linux_amd64.tar.gz` | 3,0 MB |
| `serverinfo-agent_0.1.0_linux_arm64.tar.gz` | 2,7 MB |
| `SHA256SUMS` | — |

Elke tarball bevat de binary, `install.sh`, `uninstall.sh` en de systemd-unit.
Vereist Go 1.24+ (`brew install go`); verder niets.

### 1.2 Uitrollen

```bash
scp agent/dist/serverinfo-agent_0.1.0_linux_amd64.tar.gz root@<server>:/tmp/
```

```bash
ssh root@<server> 'cd /tmp && mkdir -p si && tar xzf serverinfo-agent_*.tar.gz -C si && cd si && ./install.sh --with-extras'
```

`--with-extras` installeert `smartmontools`, `whois`, `dnsutils`, `traceroute`,
`qrencode` en `lm-sensors`. Zonder die vlag werkt de agent ook, maar dan zijn
SMART, WHOIS en de QR-code niet beschikbaar.

**Connectieprofiel kiezen** (standaard `lan`):

```bash
./install.sh --mode lan                    # alleen eigen subnet (standaard)
./install.sh --mode vpn --vpn-ip 100.64.0.5
./install.sh --mode public                 # publiek bereikbaar, mTLS beschermt
./install.sh --mode proxy                  # achter nginx op loopback
```

Het script drukt aan het eind een **koppelcode en QR-code** af.

### 1.3 Wat het script doet

1. Controleert root, systemd, architectuur en of poort 29500 vrij is
2. Maakt de systeemgebruiker `serverinfo` (nologin, geen home) aan
3. Plaatst de binary in `/usr/local/bin/`
4. Schrijft `/etc/serverinfo-agent/config.toml` passend bij het profiel
5. Installeert optionele pakketten
6. Zet een sudoers-regel voor **alleen** `smartctl` met vastgepinde argumenten
7. Genereert de CA en het servercertificaat (`bootstrap`)
8. Installeert en start de systemd-unit
9. Zet de ufw-regel passend bij het profiel
10. Doet een zelftest op `https://127.0.0.1:29500/v1/health`
11. Opent een koppelvenster van 15 minuten en toont de QR

---

## 2. De app koppelen

Op de server (als het venster verlopen is):

```bash
sudo serverinfo-agent enroll --new
```

Dat toont:

```
  Koppel dit apparaat

    Host         192.168.1.102:29500
    Koppelcode   9SQK-QQ4Z   (geldig tot 20:12)
    Fingerprint  2e2571b4…000c9f0d

    Scan deze QR in de Server Info-app:
    ███▀▀▀███  …
```

In de app: **Server → + → QR scannen**. Of *Handmatig invoeren* als je geen
camera hebt: host, poort, koppelcode en fingerprint.

Wat er dan gebeurt: de app maakt een P-256 sleutelpaar in de Keychain, stuurt
alleen de publieke sleutel, en krijgt een ondertekend client-certificaat, het
CA-certificaat en een token terug. De private sleutel verlaat het toestel nooit.
Daarna sluit het koppelvenster zichzelf.

**Vanaf dat moment weigert de agent elke TLS-handshake zonder geldig
client-certificaat.** Geverifieerd:

```
✔ zonder client-certificaat: TLS-handshake geweigerd
  (remote error: tls: certificate required)
```

---

## 3. De iOS-app bouwen en draaien

### 3.1 Vereisten

Xcode 26.6 en Swift 6.3 (aanwezig), plus `xcodegen`:

```bash
brew install xcodegen
```

### 3.2 Project genereren en bouwen

```bash
cd "Server Info/ios" && xcodegen generate
```

```bash
xcodebuild -project "Server Info/ios/ServerInfo.xcodeproj" -scheme ServerInfo -sdk iphonesimulator -destination 'platform=iOS Simulator,name=iPhone 17 Pro' build
```

### 3.3 In de simulator

```bash
xcrun simctl boot "iPhone 17 Pro"
```

```bash
xcrun simctl install booted "$(find ~/Library/Developer/Xcode/DerivedData -name 'Server Info.app' -path '*Debug-iphonesimulator*' | head -1)" && xcrun simctl launch booted nl.merlinstoll.serverinfo
```

### 3.4 Op je eigen iPhone

Open `Server Info/ios/ServerInfo.xcodeproj` in Xcode, kies je toestel, en zet
onder *Signing & Capabilities* je team. Met een gratis Apple ID vervalt de
signing na 7 dagen; met een Developer Program-account na een jaar.

---

## 4. De testronde reproduceren

### 4.1 Visuele controle (19 schermen)

```bash
cd "Server Info/ios" && xcodebuild -project ServerInfo.xcodeproj -scheme ServerInfo -sdk iphonesimulator -destination 'platform=iOS Simulator,name=iPhone 17 Pro' test
```

De UI-test loopt de hele app door en schrijft screenshots naar
`/tmp/serverinfo-shots/`. Duurt ongeveer 2,5 minuut.

### 4.2 API-controle van buitenaf

De validatieclient in `agent/` doet precies wat de app doet: sleutel maken,
koppelen, en daarna met mTLS alle endpoints aflopen — inclusief de negatieve
tests (geen certificaat, verkeerd token, `/etc/shadow` via de log-tool).

Uitgevoerd resultaat:

```
✔ enrollment  device=d_5c634d67 host=DebianG3 cert geldig tot 2027-08-20
✔ zonder client-certificaat: TLS-handshake geweigerd
✔ /v1/health              200      ✔ /v1/tools/cpuinfo       200
✔ /v1/system              200      ✔ /v1/tools/uptime        200
✔ /v1/metrics             200      ✔ /v1/tools/locale        200
✔ /v1/hardware/sensors    200      ✔ /v1/tools/updates       200
✔ /v1/hardware/smart      200      ✔ /v1/tools/processes     200
✔ /v1/hardware/gpu        200      ✔ /v1/tools/logs/sources  200
✔ /v1/hardware/disks      200      ✔ /v1/tools/logs          200
✔ /v1/hardware/network    200      ✔ /v1/devices             200
✔ verkeerd token → 401 (verwacht 401)
✔ /etc/shadow via logs → 403 (verwacht 403)
✔ SSE-stream: live samples op 1 Hz
```

---

## 5. Beheer

```bash
sudo serverinfo-agent devices list          # gekoppelde apparaten
```

```bash
sudo serverinfo-agent devices revoke d_a41f # apparaat intrekken, werkt direct
```

```bash
sudo serverinfo-agent enroll --new          # nieuw koppelvenster (15 min)
```

```bash
sudo serverinfo-agent enroll --cancel       # venster meteen sluiten
```

```bash
systemctl status serverinfo-agent
journalctl -u serverinfo-agent -f
```

---

## 6. Verwijderen

```bash
sudo /tmp/si/uninstall.sh
```

```bash
sudo /tmp/si/uninstall.sh --purge --remove-extras
```

Zonder `--purge` blijven config, CA en gekoppelde apparaten staan (handig bij
een herinstallatie). Met `--purge` gaan die weg, plus de gebruiker en de
ufw-regel. Met `--remove-extras` ook de optionele pakketten. Het script drukt
altijd af wat het níet heeft verwijderd, zodat er nooit iets stil achterblijft.

---

## 7. Gemeten op de testmachine

| | Gemeten |
|---|---|
| Binary | 7,2 MB (amd64), 6,7 MB (arm64) |
| RSS in bedrijf | 22 MB |
| CPU met één client op 1 Hz | verwaarloosbaar (< 1% van één core) |
| Idle CPU | nul, 5 minuten na de laatste client |
| Opstarttijd | < 100 ms |
| `systemd-analyze security` | 5,3 MEDIUM |
| Disk-writes | geen (behalve certificaatvernieuwing 1×/jaar) |

De securityscore is 5,3 en niet lager omdat `NoNewPrivileges` bewust uitstaat;
zie [docs/11-implementatienotities.md](docs/11-implementatienotities.md) voor
de afweging.
