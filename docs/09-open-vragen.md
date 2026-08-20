# 09 — Open vragen

Punten waar jouw keuze het ontwerp verandert. Bij elke vraag staat mijn advies, zodat je
er ook gewoon "akkoord" op kunt zeggen.

---

### OQ-1 · Drie tabs of vier? — ✅ **BESLIST (20-08-2026)**

**Vastgesteld: vier tabs — Server / Metrics / Tools / Settings.**

Hardware krijgt géén eigen tab. De statische hardware-informatie (SMART, DIMM's, NIC's,
block devices, GPU-specs) leeft als detailschermen op twee ingangen:

- vanuit **Metrics** — tik op de identiteitskaart bovenaan, of op de `GPU`/`Sensors`-knoppen;
- vanuit **Tools** — een eigen sectie *Hardware* met rijen naar dezelfde schermen.

Dat is beter dan een aparte tab, want hardware-informatie bekijk je een paar keer per
server en daarna nooit meer — dat verdient geen permanente plek in de tabbar. En de vraag
"wat is dit eigenlijk voor CPU?" stel je juist terwijl je naar de live CPU-balk kijkt,
dus die ingang hoort in Metrics te zitten.

Settings is een volwaardige tab en bevat weergave, eenheden, real-time-gedrag, privacy,
data en over-informatie — de globale defaults die je per server kunt overschrijven.

Uitgewerkt in [05 §5.2](05-ios-app.md#52-navigatiestructuur), [§5.8](05-ios-app.md#58-hardware-detailschermen)
en [§5.11](05-ios-app.md#511-tab-settings).

---

### OQ-2 · Poort 29500 — akkoord?

Ik heb de IANA-registry gedownload en geanalyseerd. **29500/tcp is aantoonbaar
unassigned**, ligt in een vrij blok van 829 opeenvolgende ongebruikte poorten
(29170–29998), zit **onder** de Linux ephemeral range (32768–60999) zodat een uitgaande
verbinding hem nooit kan inpikken, en boven 1024 zodat root niet nodig is.

Poorten uit de "dynamic" range 49152–65535 zijn juist een slechter idee dan ze klinken:
Linux gebruikt daar deels zelf ephemeral poorten en veel monitoring-tools zitten er al
(glances 61208, netdata 19999, node_exporter 9100).

**Advies: 29500, configureerbaar via `--port`.**

---

### OQ-3 · Mag de app iets *doen* op de server? — ✅ **BESLIST (20-08-2026)**

**Vastgesteld: v1 strikt read-only, inclusief de speedtest.**

Jouw terechte vraag was of read-only überhaupt kan als we een speedtest willen. Dat kan,
want "read-only" slaat op **systeemtoestand**, niet op "start nooit een proces". Speedtest,
ping, dig, whois en traceroute veranderen niets aan de machine en laten niets achter dat een
herstart overleeft. Volledige uitwerking in
[07 §7.1](07-security.md#71-threat-model).

Wat er dus **niet** in komt: services herstarten, `apt upgrade`, reboot, configuratie
wijzigen. Als je dat later wilt: achter een aparte `allow_actions = true`, per-actie
whitelist en Face ID-bevestiging in de app.

Eén maatregel die hieruit volgde: speedtest is apart begrensd op **1 run per 5 minuten**,
omdat elke run 1–3 GB verkeer kost en dat anders een manier is om je datalimiet op te maken.

---

### OQ-4 · Bereikbaarheid van buiten — ✅ **BESLIST (20-08-2026)**

Jouw antwoord: in de basis lokaal; `a.mest.dev` (VPS) eventueel direct op het externe IP;
verder VPN voor apparaten achter een firewall.

**Vastgesteld: de agent krijgt vier connectieprofielen**, te kiezen met
`install.sh --mode`. Uitgewerkt in [01 §1.5](01-architectuur.md#15-netwerktopologie--hoe-bereikt-de-telefoon-de-server)
en [07 §7.6](07-security.md#76-netwerkblootstelling).

| Profiel | Voor | TLS |
|---------|------|-----|
| `lan` (default) | alles thuis/kantoor | eigen CA |
| `vpn` | apparaten achter een firewall | eigen CA |
| `public` | `a.mest.dev` | eigen CA |
| `proxy` | als je per se alles op 443 wilt | nginx' publieke certificaat |

**Herzien op 20-08-2026:** met de komst van mTLS ([10](10-device-enrollment.md)) is
`public` het advies voor `a.mest.dev` geworden, niet `proxy` — zie de noot onderaan.
De oorspronkelijke afweging stond zo: Ik heb gekeken wat daar draait:
nginx met HTTP/2 en een geldig publiek certificaat, en **poort 80 is dicht**. Dus:

- Je hoeft geen tweede poort open te zetten; de agent bindt op `127.0.0.1` en is van buiten
  domweg onbereikbaar behalve via nginx.
- Je certificaat is al geldig en al geautomatiseerd — de app gebruikt gewoon de systeem-CA's,
  geen pinning en geen ATS-uitzondering nodig. Dat scheelt werk én is veiliger.
- Zou je tóch `public` willen: poort 80 is dicht, dus ACME kan daar **niet** via HTTP-01.
  Dan wordt het DNS-01 of hergebruik van nginx' certificaat.

**Waarom het advies is omgedraaid.** Met mutual TLS heeft de agent helemaal geen publiek
vertrouwd certificaat meer nodig: de app valideert tegen de CA die hij bij koppeling
ontving. Daarmee verdwijnt de enige reden om achter nginx te kruipen, en houd je één
identiek mechanisme voor al je servers in plaats van twee. `proxy` blijft beschikbaar voor
als je op restrictieve netwerken zit die alleen 443 doorlaten.

Twee dingen die daaruit volgden en nu in het ontwerp zitten:

1. `a.mest.dev` gebruikt HTTP Basic Auth, die dezelfde `Authorization`-header claimt als ons
   bearer-token. De agent accepteert het token daarom **ook** via `X-Server-Info-Token`
   ([03 §3.0](03-api-contract.md#30-authenticatie)). Je kunt de basic auth dan gewoon laten
   staan als extra laag.
2. `a.mest.dev` heeft alleen een A-record, geen AAAA. Voer voor remote hosts altijd de
   **hostname** in en niet `194.163.173.139` — op IPv6-only mobiele netwerken werkt de
   hostname via NAT64/DNS64 wél en een IPv4-literal niet. De app waarschuwt daarvoor.

---

### OQ-5 · Hoeveel servers, en wil je een overzicht van allemaal tegelijk?

Bij 2–5 servers is de huidige opzet (één geselecteerde server) perfect. Bij 15+ wil je
waarschijnlijk een dashboard met alle servers naast elkaar.

**Advies:** bouw v1 met één geselecteerde server. Als je er meer dan ~8 hebt, voeg dan in
v1.1 een compacte grid-weergave toe aan de Server-tab.

---

### OQ-6 · Speedtest: welke CLI?

Ookla is accurater en geeft een deelbare resultaat-URL, maar vereist licentie-acceptatie
en een extra apt-repo. librespeed-cli is één binary, open source, geen repo nodig.

**Advies: Ookla als eerste keuze, librespeed als fallback.** `install.sh --with-extras`
probeert Ookla en valt terug op librespeed als de repo niet beschikbaar is (bijv. op
oudere Debian of op ARM).

---

### OQ-7 · Proceslijst? — ✅ **BESLIST (20-08-2026)**

**Vastgesteld: ja, maar alleen als tool-scherm.** Niet als sectie op Metrics.

Onder Tools → Systeem → *Processes*: volledige lijst, sorteerbaar op CPU / geheugen / naam,
met zoekveld en een balkje per regel. Zo blijft Metrics compact en is de vraag "waardoor is
mijn RAM 89%?" nog steeds twee tikken weg.

---

### OQ-8 · Historie: 5 minuten of langer?

Nu houdt de agent 300 samples (5 min) in geheugen. Voor "wat gebeurde er vannacht om 3
uur" heb je persistente opslag nodig (SQLite, ~1 MB/dag op 1 min-resolutie).

**Advies: v1 met 5 minuten in geheugen.** Het is precies genoeg voor de grafieken die je
beschrijft, kost nul disk-writes en houdt de agent echt lightweight. Trendhistorie is een
duidelijk afgebakende v1.1-feature.

---

### OQ-9 · Distributie van de agent — ✅ **BESLIST (20-08-2026)**

**Vastgesteld: private GitHub-repo voor de broncode.**

Aandachtspunt dat daaruit volgt: een private repo kan het one-liner installatiecommando
niet zonder token serveren. `curl -fsSL https://raw.githubusercontent.com/…` vraagt bij een
private repo om authenticatie, en dat is het tegenovergestelde van "klik en klaar".

**Advies: broncode privé op GitHub, en de installer + tarballs serveren vanaf `a.mest.dev`.**
Daar staat al nginx met een geldig certificaat. Eén `location /si/` zonder basic auth,
met daarin `install.sh`, de twee tarballs en `SHA256SUMS`, geeft je:

```bash
curl -fsSL https://a.mest.dev/si | sudo bash
```

Het script verifieert de SHA256 van de tarball voordat het iets uitpakt. `make release`
bouwt, ondertekent de checksums en rsynct naar die map — één commando om een nieuwe versie
uit te rollen.

*Alternatieven:* een publieke GitHub-repo (dan werkt Releases direct, maar je code is
openbaar), of tarballs met `scp` blijven kopiëren (prima bij 2–3 servers, vervelend daarna).

---

### OQ-10 · Naam en bundle-identifier

Nodig voor het Xcode-project. Voorstel: app "Server Info", bundle-id
`nl.merlinstoll.serverinfo`, agent-binary `serverinfo-agent`, URL-scheme `serverinfo://`.
