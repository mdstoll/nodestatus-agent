# 07 — Security

Een monitoring-agent is een aantrekkelijk doelwit: hij draait op al je servers, kent alle
systeemdetails en kan commando's uitvoeren. Dit hoofdstuk is daarom geen bijlage maar
onderdeel van het ontwerp.

## 7.1 Threat model

| Aanvaller | Kan | Mitigatie |
|-----------|-----|-----------|
| Iemand op hetzelfde LAN | Poort 29500 scannen, requests sturen | **mTLS**: zonder gekoppeld client-certificaat faalt de handshake — geen enkel endpoint reageert |
| Passieve afluisteraar | Verkeer meelezen | TLS 1.3 |
| Actieve MITM | Zich voordoen als de server | Servercert wordt gevalideerd tegen de CA die de app bij enrollment ontving |
| Internet-scanner (Shodan e.d.) | De poort vinden | Standaard advies: bind op VPN/localhost; `allow_cidr`; bij publieke blootstelling fail2ban + strenge auth-limits |
| Iemand met de app / een verloren telefoon | Alles wat de app kan | Sleutel+token in Keychain `.whenUnlockedThisDeviceOnly`; optionele Face ID-lock; **apparaat op afstand intrekken** met `devices revoke` |
| Een andere app die onze endpoint probeert | Data uitlezen, versie-info oogsten | mTLS (laag 1) — zie [10 — Device enrollment](10-device-enrollment.md) |
| Lokale niet-root gebruiker op de server | Token lezen | Config `chmod 600`, owner `serverinfo` |
| Aanvaller die de agent misbruikt | Commando's injecteren, willekeurige bestanden lezen | Whitelists + argv-arrays, zie §7.4 |

### Wat "read-only" hier precies betekent

Read-only slaat op **systeemtoestand**, niet op "start nooit een proces". Dat onderscheid is
belangrijk, want een speedtest, ping en traceroute zijn per definitie processen — en ze zijn
toch read-only in de enige betekenis die er voor security toe doet:

| | Verandert iets aan het systeem? | Overleeft het een herstart? |
|---|---|---|
| `/proc`, `/sys` lezen | nee | n.v.t. |
| `smartctl -A`, `nvidia-smi` | nee | nee |
| `journalctl`, `tail` | nee | nee |
| `apt-get -s dist-upgrade` (simulatie) | nee | nee |
| `speedtest`, `ping`, `dig`, `whois`, `traceroute` | nee | nee |
| ~~service herstarten, `apt upgrade`, reboot~~ | **ja** | **ja** — niet in v1 |

Geen enkele endpoint schrijft naar het filesystem, installeert iets, wijzigt configuratie,
start of stopt services, of laat iets achter dat een herstart overleeft. Een aanvaller met
een gekoppeld certificaat kan dus **lezen en diagnosticeren**, niet **veranderen of
persisteren**. Dat is precies het verschil dat de impact van een incident begrenst.

**Wat de agent expliciet níet doet:** geen shell, geen willekeurige commando's, geen
schrijven naar het systeem, geen pakketten installeren, geen services herstarten,
geen bestanden buiten de whitelist lezen.

De prijs die je wél betaalt voor de diagnostische tools is een groter aanvalsoppervlak dan
puur bestanden lezen — vandaar de vier harde regels in [§7.4](#74-commando-uitvoering--de-belangrijkste-regel)
(argv-arrays, strikte validatie, absolute paden, verplichte timeouts). En één specifiek
risico: een speedtest verbrandt 1–3 GB verkeer. Iemand die hem herhaaldelijk aanroept, kan
je datalimiet opmaken. Daarom is speedtest apart begrensd op **1 run per 5 minuten per
server**, bovenop de algemene job-limieten.

## 7.2 TLS

Alle verbindingen zijn **mutual TLS 1.3** met certificaten uit de per-server CA. De
volledige uitwerking staat in [10 — Device enrollment](10-device-enrollment.md); hier de
TLS-parameters zelf.

- **ECDSA P-256**, SHA-256. CA en servercertificaat 10 jaar, client-certificaten 1 jaar
  met automatische vernieuwing.
- SAN's op het servercertificaat voor: hostname, `localhost`, `127.0.0.1` en alle
  niet-loopback IP's die bij installatie bestaan. Bij een IP-wijziging:
  `serverinfo-agent regenerate-cert` (de CA blijft, dus gekoppelde apparaten blijven werken).
- `MinVersion: tls1.3`, HTTP/2 uit (SSE werkt prima op HTTP/1.1 en het scheelt complexiteit).
- `ClientAuth` is **dynamisch** via `GetConfigForClient`: `RequireAndVerifyClientCert` in
  rust, `RequestClientCert` alleen tijdens een open enrollment-venster.
- De CA-private key wordt na het genereren van het servercertificaat alleen nog gebruikt om
  CSR's te ondertekenen — nooit voor TLS zelf.
- **App-kant:** het servercertificaat wordt gevalideerd tegen het CA-certificaat dat de app
  bij koppeling ontving (`SecTrustSetAnchorCertificates`), niet tegen de systeem-CA's en
  niet via TOFU. Dat is strenger dan pinning en tegelijk onderhoudsvrij, want een
  vernieuwd servercertificaat van dezelfde CA blijft gewoon geldig.
- ATS: TLS 1.3 met ECDSA voldoet aan de eisen; de trust-evaluatie wordt door de delegate
  overgenomen. `NSAllowsLocalNetworking` blijft nodig voor LAN-adressen.

## 7.3 Authenticatie

Twee lagen. De eerste — het client-certificaat — staat volledig uitgewerkt in
[10 — Device enrollment](10-device-enrollment.md). Hieronder de tweede laag.

- Token: 32 bytes uit `crypto/rand`, base64url → 43 tekens, per gekoppeld apparaat uniek.
- Vergelijking met `subtle.ConstantTimeCompare` — nooit `==`.
- Fout wachtwoord → `401` met een **vaste vertraging van 250 ms** en een teller per IP;
  na 10 fouten in 60 s een `429` van 5 minuten.
- Token roteren: `serverinfo-agent rotate-token` print een nieuwe token + QR; de app
  merkt de 401 en biedt "Opnieuw koppelen" aan.
- Geen sessies, geen cookies, geen refresh tokens. Voor een persoonlijke tool is een
  bearer token over gepind TLS de juiste hoeveelheid complexiteit.

## 7.4 Commando-uitvoering — de belangrijkste regel

Elke plek waar de agent een extern programma start, volgt dezelfde vier regels:

1. **Nooit een shell.** `exec.CommandContext(ctx, "/usr/bin/ping", "-c", "10", target)` —
   een argv-array. Geen `sh -c`, geen string-interpolatie, geen `os/exec` met een
   samengesteld commando. Hiermee is shell-injectie structureel onmogelijk, niet
   "hopelijk wegge-escaped".
2. **Valideer elk argument tegen een strikt patroon.**
   - hostname: RFC-1123-regex, max 253 tekens, of een geldig IP via `netip.ParseAddr`
   - aantallen: integer binnen een bereik (`count` 1–20, `lines` 1–500)
   - enums (recordtype, priority, source): lookup in een vaste map, niet doorgeven
   - **Blokkeer interne targets** voor ping/dns/traceroute optioneel (`127.0.0.0/8`,
     `169.254.0.0/16`, cloud-metadata `169.254.169.254`) om SSRF-achtig misbruik te
     voorkomen; configureerbaar, want soms wil je juist intern pingen.
3. **Absolute paden**, opgezocht bij het starten van de agent, niet uit `PATH` op het moment
   van uitvoeren. `PATH` in de child-omgeving wordt leeggemaakt.
4. **Altijd een context met timeout** en een cap op de output (1 MB), zodat een
   vastlopend commando de agent niet meesleurt.

Voor logbestanden geldt hetzelfde principe: het pad moet **exact** in de whitelist staan.
Geen prefix-match, geen `filepath.Join` met gebruikersinvoer, geen symlink-volgen
(`O_NOFOLLOW`). Anders is `../../etc/shadow` een kwestie van tijd.

## 7.5 Rechten op de server

- Draait als `serverinfo` (systeemgebruiker, `nologin`, geen home).
- Lid van `systemd-journal` → journal lezen zonder root.
- `sudoers.d/serverinfo-agent` bevat **alleen** dit, met volledige paden en argumenten:
  ```
  serverinfo ALL=(root) NOPASSWD: /usr/sbin/smartctl -j -A -H /dev/sd[a-z], \
                                  /usr/sbin/smartctl -j -A -H /dev/nvme[0-9]n[0-9]
  ```
  `nvidia-smi` en `speedtest` hebben geen root nodig. `intel_gpu_top` wel — die laten we
  standaard uit.
- systemd-hardening zoals in [02 §2.4](02-worker-daemon.md): `ProtectSystem=strict`,
  `NoNewPrivileges`, `SystemCallFilter=@system-service`, `MemoryMax=128M`.
- Controleer je hardening met `systemd-analyze security serverinfo-agent` — streef naar
  een score onder 3.0 ("OK" of beter).

## 7.6 Netwerkblootstelling

Omdat mTLS in álle profielen geldt, is de vraag "hoe erg is het als iemand de poort vindt?"
grotendeels beantwoord: zonder gekoppeld client-certificaat komt er geen HTTP-verzoek
doorheen en is er niets te oogsten. Wat overblijft is bereikbaarheid en ruis.

| Profiel | Blootstelling | Extra vereisten |
|---------|---------------|-----------------|
| **B `vpn`** | geen | niets |
| **A `lan`** | eigen subnet | `allow_cidr` + firewallregel op je subnet |
| **C `public`** | poort 29500 op internet | fail2ban, strengere auth-limits |
| **D `proxy`** | via nginx op 443/8443 | `ssl_verify_client on` in een **apart** server-blok |

### Profiel C — publiek (advies voor `a.mest.dev`)

- **`allow_cidr` helpt niet**: een telefoon op 4G/5G heeft een dynamisch IP. Het
  client-certificaat is je grens, en dat is een sterkere grens dan een IP-filter.
- **fail2ban.** De agent logt afgewezen handshakes en auth-fouten naar journald in een vast
  formaat: `serverinfo-agent: auth failure from <ip>`. Filter van vier regels + een jail
  (3 pogingen / 10 min → 1 uur ban) houdt de logs schoon en stopt geautomatiseerd geklop.
- **Strengere ingebouwde limieten** in dit profiel: 5 auth-fouten per IP per 10 min, en een
  cap op gelijktijdige verbindingen per IP.
- **Poort 29500 is geen beveiliging**, alleen ruisreductie. Ga ervan uit dat hij gevonden
  wordt — en dat dat niet erg is.
- **Enrollment-vensters kort houden.** Dit is het enige moment waarop de agent iets zegt
  tegen een onbekende client. 15 minuten, 5 pogingen, en het venster sluit zichzelf na een
  geslaagde koppeling.

### Profiel D — achter nginx

Alleen als je alles op 443 wilt. Aandachtspunten:

1. **Een apart `server {}`-blok.** `ssl_verify_client on` geldt per server-blok; op je
   bestaande vhost zetten betekent dat elke browserbezoeker een certificaatkiezer krijgt.
2. **`proxy_buffering off` en `proxy_read_timeout 1h`** — zonder die twee schokt of
   verbreekt de SSE-stream.
3. **`trusted_proxy = ["127.0.0.1"]`** in de agentconfig, zodat de rate limiter het echte
   client-IP uit `X-Forwarded-For` haalt. Alleen zetten als er écht een proxy voor staat,
   anders kan een client zijn eigen `X-Forwarded-For` verzinnen.
4. **Token via `X-Server-Info-Token`** als de vhost al basic auth gebruikt.

### Firewall

`install.sh` zet de ufw-regel passend bij het profiel:

| Profiel | Regel |
|---------|-------|
| `lan` | `ufw allow from 192.168.1.0/24 to any port 29500 proto tcp` |
| `vpn` | `ufw allow in on wg0 to any port 29500 proto tcp` |
| `public` | `ufw allow 29500/tcp` + expliciete bevestiging in het script |
| `proxy` | geen regel voor 29500 — loopback heeft er geen nodig |

Profiel `lan` zet dus géén poort open naar de hele wereld: de regel is gebonden aan je
eigen subnet. Kleine moeite, scheelt een categorie ongelukken.

## 7.7 App-kant

- Tokens en fingerprints in de **Keychain**, `kSecAttrAccessibleWhenUnlockedThisDeviceOnly`
  — geen iCloud-sync van servertokens.
- Optionele Face ID-lock op de app (`LAContext`), standaard uit.
- Geen analytics, geen crash-reporting naar derden, geen netwerkverkeer naar iets anders
  dan je eigen servers. Dat is te controleren en dat is het punt.
- Serienummers en publieke IP's worden **gemaskeerd** getoond (`84.28.x.x`) met een
  tik-om-te-tonen, zodat een screenshot van de app niet meteen alles weggeeft.
- Bij het delen van een screenshot/resultaat: hostnames en IP's optioneel anonimiseren.

## 7.8 Wat er nog niet in zit (bewust)

*(mTLS stond hier eerst als "later" — dat is per 20-08-2026 naar voren gehaald en zit nu in v1, zie [10](10-device-enrollment.md).)*

| Niet in v1 | Waarom | Wanneer wel |
|------------|--------|-------------|
| Meerdere gebruikers/rollen | Eén gebruiker, één token | Bij team-gebruik |
| Schrijf-acties (restart service, apt upgrade) | Vergroot de impact van een gestolen token enorm | Alleen achter een aparte, expliciet in te schakelen `allow_actions`-flag + Face ID-bevestiging |
| Audit-log | Overkill voor één gebruiker | Bij team-gebruik |
