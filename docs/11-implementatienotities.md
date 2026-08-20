# 11 — Implementatienotities

Waar de gebouwde versie afwijkt van de analyse, en waarom. Elk punt is een
beslissing die tijdens de bouw is genomen omdat de werkelijkheid iets anders
bleek dan op papier.

---

## 11.1 Geen CSR, maar een publieke sleutel met bewijs van bezit

**Analyse ([10 §10.3](10-device-enrollment.md)):** de app maakt een PKCS#10 CSR
en de agent ondertekent die.

**Gebouwd:** de app stuurt het kale publieke punt (X9.63, 65 bytes) en de agent
bouwt daar zelf een certificaat omheen.

**Waarom:** een CSR maken in Swift betekent met de hand ASN.1/DER schrijven —
er is geen API voor. Dat is ~150 regels foutgevoelige bytecode voor precies
nul extra beveiliging: de private sleutel blijft in beide gevallen in de
Keychain, en de agent bepaalt toch alle velden van het certificaat zelf. De
CSR-handtekening zou alleen bewijzen dat de client de sleutel bezit, en dat is
hier niet interessant, want een aanvaller wint niets door een certificaat aan
te vragen voor een sleutel die hij niet heeft.

---

## 11.2 Servercertificaat: 397 dagen, met automatische vernieuwing

**Analyse:** servercertificaat 10 jaar geldig.

**Gebouwd:** 397 dagen, met vernieuwing zodra er nog 30 dagen over zijn.

**Waarom:** iOS weigert het gewoonweg. Letterlijke fout uit de app:

```
Certificate 0 "DebianG3" has errors:
Certificate exceeds maximum temporal validity period
```

Apple hanteert sinds iOS 13 een maximum van 398 dagen voor TLS-*server*­certificaten,
ook als ze door een CA komen die het toestel expliciet vertrouwt. Voor de CA
zelf geldt die regel niet, dus die is nog steeds 10 jaar geldig — en dat is wat
telt, want een gekoppeld apparaat blijft daardoor gekoppeld.

Gevolg: de agent moet zijn eigen certificaat kunnen vervangen. Daarvoor is
`ReadWritePaths=/etc/serverinfo-agent` toegevoegd aan de systemd-unit, en houdt
een `CertManager` het actieve certificaat vast met `tls.Config.GetCertificate`,
zodat een vernieuwing meteen geldt zonder herstart. Er wordt elke 12 uur
gekeken; in de praktijk schrijft de agent dus één keer per jaar naar disk.

---

## 11.3 `NoNewPrivileges` staat uit

**Analyse ([02 §2.4](02-worker-daemon.md)):** `NoNewPrivileges=yes`.

**Gebouwd:** `NoNewPrivileges=no`, plus lidmaatschap van de groep `disk`.

**Waarom:** SMART op NVMe vereist `NVME_IOCTL_ADMIN_CMD`, en dat vereist
`CAP_SYS_ADMIN`. Met `NoNewPrivileges=yes` kan de agent niet via sudo
escaleren, en dan levert `smartctl` dit op:

```
Read NVMe SMART/Health Information failed:
NVME_IOCTL_ADMIN_CMD: Permission denied
```

Er waren twee wegen:

| Optie | Wat de agent krijgt |
|---|---|
| `AmbientCapabilities=CAP_SYS_ADMIN` | vrijwel root, permanent, voor het hele proces |
| `NoNewPrivileges=no` + sudoers-whitelist | precies één commando met vastgepinde argumenten |

De tweede is smaller, ook al ziet `NoNewPrivileges=no` er in een audit erger
uit. De sudoers-regel staat exact één ding toe:

```
serverinfo ALL=(root) NOPASSWD: /usr/sbin/smartctl -j -A -H -i /dev/nvme[0-9]n[0-9], …
```

`systemd-analyze security` geeft hierdoor 5,3 (MEDIUM) in plaats van ~3.
Dat is een bewuste ruil, geen omissie.

---

## 11.4 De sampler blijft 5 minuten nadraaien

**Analyse ([01 §1.3](01-architectuur.md)):** de sampler stopt zodra er geen
SSE-client meer is, zodat idle-CPU nul is.

**Gebouwd:** hij stopt pas 5 minuten na de laatste client.

**Waarom:** met onmiddellijk stoppen is de ringbuffer bij élke nieuwe
verbinding leeg, en dan doet `?backfill=60` niets. De netwerkgrafiek zou dan
telkens een minuut lang leeg opbouwen — precies wat backfill moest voorkomen.
Vijf minuten nadraaien kost verwaarloosbaar weinig en zorgt dat je bij het
opnieuw openen van de app meteen een gevulde grafiek hebt. Een server waar
niemand naar kijkt, valt na die vijf minuten alsnog terug naar nul.

---

## 11.5 Netwerkmounts tellen niet mee in de opslag

Niet in de analyse voorzien, wel meteen zichtbaar op de testmachine: die heeft
negen `fuse.rclone`- en `cifs`-mounts, samen goed voor 45 TB aan clouddrives.
Zonder filtering zou "Storage" dat als lokale opslag rapporteren.

De agent markeert `cifs`, `nfs`, `smbfs`, `sshfs` en alle `fuse.*` als
`remote: true`. Die worden wél getoond (in het opslagdetail, apart gegroepeerd)
maar tellen niet mee in het totaal. Hetzelfde principe geldt voor btrfs-subvolumes,
die anders dubbel zouden tellen: er wordt gededupliceerd op device.

---

## 11.6 Virtuele netwerkinterfaces tellen niet mee

Ook uit de praktijk: de testmachine heeft acht `veth`-interfaces, een
`docker0`-bridge en een `tun0`. Hun verkeer loopt óók over `eth0`, dus
meetellen zou alles dubbel of driedubbel maken.

`lo`, `veth*`, `docker*`, `br-*`, `virbr*`, `tun*`, `tap*`, `wg*` en de
container-netwerken worden uitgesloten van het totaal, maar blijven zichtbaar
in de interfacelijst — je wilt kunnen zien dát ze er zijn.

---

## 11.7 Onzinnige sensordrempels worden weggegooid

De NVMe-schijf in de testmachine rapporteert:

```
/sys/class/hwmon/hwmon1/temp2_max = 65261850   →  65.261 °C
```

Een sentinelwaarde die "geen drempel ingesteld" betekent. Klakkeloos overnemen
zorgde ervoor dat een schijf van 85,8 °C als `ok` gold. Drempels buiten een
plausibel bereik (≤ 0 of > 150 °C) worden nu genegeerd, waarna de
fallback-logica het overneemt.

Tegelijk is de waarschuwingsgrens aangescherpt: warm vanaf 85% van de kritieke
waarde in plaats van 90%. Bij een kritieke grens van 100 °C is 85 °C al reden
om te kijken, niet pas 99 °C.

---

## 11.8 Twee bugs die iOS-specifiek waren

**`SecIdentityCreateWithCertificate` bestaat niet op iOS.** Dat is een
macOS-only API. Op iOS vormt de Keychain zelf een identity zodra certificaat én
private sleutel er allebei in staan; je vindt hem door alle identities op te
vragen en de certificaat-DER te vergelijken.

**De SSE-stream vraagt zijn TLS-challenge op taakniveau.** Gewone requests via
`URLSession.data(for:)` komen bij `urlSession(_:didReceive:)` uit, maar
`URLSession.bytes(for:)` gebruikt `urlSession(_:task:didReceive:)`. Met alleen
de eerste viel de stream terug op de standaardvalidatie en werd ons eigen
CA-certificaat geweigerd — terwijl alle andere endpoints prima werkten. Beide
methodes zijn nu geïmplementeerd.

---

## 11.9 Lege arrays, geen null

Go serialiseert een nil-slice als `null`. Een server zonder GPU stuurde dus
`"gpu": null`, waarop de Swift-decoder de hele sample liet vallen — het scherm
bleef leeg terwijl er niets mis was.

Twee kanten opgelost: de agent initialiseert alle slices als leeg, én de app
decodeert `null` als lege array (`@DefaultEmpty`). Die tweede laag is er zodat
de app niet stukgaat op een oudere agent.

---

## 11.10 Geheugengebruik: 22 MB in plaats van 15

De analyse mikte op < 15 MB RSS. Gemeten: **22 MB**. Het verschil zit in de
Go-runtime plus de proceslijst-snapshot, die kortstondig een map met alle
PID's aanlegt. Dat is nog steeds een orde van grootte minder dan Netdata
(~150 MB) en het haalt de bedoeling — maar het cijfer in de analyse was te
optimistisch en is hier gecorrigeerd.

---

## 11.11 Testvoorzieningen in de app

Twee launch-argumenten bestaan **alleen in DEBUG-builds** (`#if DEBUG`) en
komen dus nooit in een release terecht:

| Argument | Doel |
|---|---|
| `-SIPairURL <serverinfo://enroll?…>` | automatisch koppelen bij het starten |
| `-SITab server\|metrics\|tools\|settings` | op een bepaald tabblad starten |

Ze bestaan omdat de simulator geen camera heeft en `simctl` niet kan tikken.
De UI-test in `ios/ServerInfoUITests/` gebruikt ze niet meer — die tikt echt —
maar ze blijven handig om een scherm snel te reproduceren.
