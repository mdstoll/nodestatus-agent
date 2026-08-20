# 10 — Device enrollment & toegangsbeheer

> Dit hoofdstuk vervangt de TLS-keuzes uit [01 §1.5](01-architectuur.md) en
> [07 §7.2](07-security.md) waar ze afwijken. De aanleiding: alleen een bearer token
> betekent dat *elke* client die het token bemachtigt of raadt volledig binnen is, en dat
> een willekeurige scanner tenminste kan zien dát er iets draait. Dat is niet goed genoeg.

## 10.1 Het probleem in één zin

Een endpoint dat een TCP-verbinding accepteert en pas op HTTP-niveau "nee" zegt, heeft al
verteld dat hij bestaat, welk TLS-profiel hij heeft en vaak ook welke software hij draait.
We willen dat een niet-gekoppelde client **niet eens een TLS-handshake kan voltooien**.

## 10.2 De oplossing: elke server is zijn eigen mini-CA

Bij installatie genereert de agent drie dingen:

| Artefact | Waar | Levensduur |
|----------|------|-----------|
| **CA-sleutel + CA-certificaat** | `/etc/serverinfo-agent/ca/` — `chmod 600`, verlaat de machine nooit | 10 jaar |
| **Servercertificaat**, getekend door die CA | `/etc/serverinfo-agent/` | 10 jaar |
| **Enrollment-code**, eenmalig, 15 min geldig | alleen in geheugen + op het scherm | 15 min |

Er is geen globale CA, geen externe PKI, geen Let's Encrypt, geen ACME. Elke server
vertrouwt precies de apparaten die jij er zelf aan hebt gekoppeld — en niets anders.

**Vier onafhankelijke lagen**, van buiten naar binnen:

| Laag | Beschermt tegen | Faalt hoe |
|------|-----------------|-----------|
| 1 · **Client-certificaat (mTLS)** | Elke niet-gekoppelde client, inclusief scanners | TLS-handshake wordt afgebroken. Geen HTTP, geen banner, geen versienummer, geen foutmelding met inhoud |
| 2 · **Fingerprint-allowlist** | Een ingetrokken of gestolen apparaat | 403, direct effect, geen CRL nodig |
| 3 · **Bearer token** | Een gelekt certificaat zonder token | 401 |
| 4 · **Servercert-validatie tegen de CA** | Man-in-the-middle / nagemaakte server | App weigert de verbinding |

Laag 1 is de laag die jouw vraag beantwoordt. Een andere app die onze endpoint probeert te
benaderen, krijgt geen `401` met uitleg — die krijgt `tls: bad certificate` en verder niets.

## 10.3 De koppelflow ("klik en klaar")

```
┌─ op de server ────────────────────────────────────────────────┐
│  $ curl -fsSL https://get.<jouwdomein>/si | sudo bash          │
│                                                                │
│  ✔ serverinfo-agent draait op web-01                           │
│                                                                │
│    Koppelcode  K7QM-3XR9      (geldig tot 16:47)               │
│                                                                │
│    ███▀▀▀███  ▀█ ██▀                                           │
│    █ ███ █ ▀█▀▄ ▄▀█   ← scan dit in de Server Info-app         │
│    █▄▄▄▄▄█ █▀ ▀█ ██                                            │
└────────────────────────────────────────────────────────────────┘
```

1. **Server** — één commando. Het script installeert de agent, genereert de CA, en opent
   een **enrollment-venster van 15 minuten**. De QR bevat host, poort, de SHA-256 van het
   servercertificaat en de eenmalige code — géén sleutelmateriaal.
2. **App** — `+` → *Server koppelen* → camera → QR scannen. Meer doet de gebruiker niet.
3. **Onder water**:
   - De app genereert een P-256 sleutelpaar in de Keychain (verlaat het toestel nooit).
   - De app maakt een CSR met CN = apparaatnaam ("iPhone van Merlin").
   - `POST /v1/enroll { code, csr_pem, device_name }` over TLS, met het servercertificaat
     gevalideerd tegen de fingerprint uit de QR.
   - De agent controleert de code (constant-time, eenmalig, TTL), tekent de CSR met zijn CA
     en antwoordt met `{ client_cert_pem, ca_cert_pem, api_token, device_id }`.
   - De agent noteert het apparaat in `/var/lib/serverinfo-agent/devices.json`, **verbrandt
     de code en sluit het enrollment-venster**.
   - De app bewaart identiteit + token in de Keychain.
4. Vanaf dat moment eist de agent voor élke verbinding een geldig client-certificaat.

Totale gebruikersactie: één commando plakken, één QR scannen.

## 10.4 Waarom een enrollment-venster en niet permanent open

De TLS-configuratie is **dynamisch** (`tls.Config.GetConfigForClient`), zonder de listener
te herstarten:

| Toestand | `ClientAuth` | Wat een vreemde client ziet |
|----------|--------------|------------------------------|
| Geen venster open (normaal) | `RequireAndVerifyClientCert` | Handshake mislukt. Niets. |
| Venster open (15 min) | `RequestClientCert` | Alleen `/v1/enroll` reageert, en alleen op de juiste code |

Je bent dus 15 minuten per gekoppeld apparaat licht zichtbaar, en de rest van de tijd
onzichtbaar. Een nieuw venster open je bewust:

```bash
sudo serverinfo-agent enroll --new        # print nieuwe code + QR, 15 min geldig
sudo serverinfo-agent enroll --cancel     # venster direct sluiten
```

De code is 8 tekens Crockford-base32 (`K7QM3XR9`) met checksum: ~40 bits entropie, en de
agent staat **maximaal 5 pogingen per venster** toe voordat hij het venster sluit.
Raden is daarmee geen aanvalsroute.

## 10.5 Meerdere apparaten, en er weer af

Je koppelt gewoon meerdere apparaten (iPhone, iPad, een tweede telefoon) — elk krijgt zijn
eigen certificaat, met een eigen `device_id`. Dat is precies het punt van deze opzet: je
kunt er één intrekken zonder de rest te raken.

```bash
$ sudo serverinfo-agent devices list
ID        NAAM                 GEKOPPELD     LAATST GEZIEN   VERLOOPT
d_a41f    iPhone van Merlin    2026-08-20    2 min geleden   2027-08-20
d_9c02    iPad                 2026-08-22    3 dagen geleden 2027-08-22

$ sudo serverinfo-agent devices revoke d_9c02
✔ iPad ingetrokken. Bestaande verbindingen verbroken.
```

Intrekken werkt via een **allowlist op certificaat-fingerprint**, niet via een CRL. Dat is
simpeler, heeft geen distributieprobleem en werkt direct — ook op openstaande verbindingen.

In de app staat dezelfde lijst onder **Settings → Gekoppelde apparaten** per server, met een
swipe-om-in-te-trekken. Je huidige toestel is gemarkeerd en kan zichzelf niet intrekken
zonder bevestiging.

## 10.6 Vernieuwing

Client-certificaten zijn **1 jaar** geldig. Vanaf 30 dagen voor het verlopen vernieuwt de
app automatisch via `POST /v1/devices/me/renew`, geauthenticeerd met het nog geldige
certificaat. Zonder dit breekt de app precies één jaar na installatie zonder duidelijke
reden — een klassieke en vervelende bug, dus dit hoort vanaf dag één in het ontwerp.

Is een certificaat tóch verlopen: de app detecteert dat en biedt "Opnieuw koppelen" aan,
waarvoor je een nieuw venster op de server opent.

## 10.7 Gevolgen voor de vier connectieprofielen

mTLS verandert de afweging uit [01 §1.5](01-architectuur.md). Het servercertificaat hoeft
**niet meer publiek vertrouwd** te zijn, want de app krijgt bij enrollment de CA mee en kan
daar netjes tegen valideren — dus ook geen TOFU meer.

| Profiel | Servercert | Toegang | Advies |
|---------|-----------|---------|--------|
| `lan` | eigen CA | mTLS | Ongewijzigd, nu sterker |
| `vpn` | eigen CA | mTLS | Ongewijzigd, nu sterker |
| `public` | eigen CA | mTLS | **Nu ook veilig genoeg voor `a.mest.dev`** |
| `proxy` | nginx' certificaat | nginx doet `ssl_verify_client` | Alleen als je per se alles op 443 wilt |

**Herziening voor `a.mest.dev`:** vorige ronde adviseerde ik `proxy` achter de bestaande
nginx, omdat dat het certificaatprobleem oploste. Met mTLS is dat probleem er niet meer, en
weegt de eenvoud zwaarder: **`public` met mTLS op 29500** betekent één mechanisme voor al je
servers, geen nginx-afhankelijkheid, geen ACME, geen renewals.

Wil je tóch alles op poort 443 (handig op restrictieve gastnetwerken die niets anders
doorlaten), gebruik dan een **apart `server {}`-blok**, niet je bestaande vhost:

```nginx
server {
    listen 8443 ssl;                       # of een eigen hostname op 443
    server_name a.mest.dev;

    ssl_client_certificate /etc/serverinfo-agent/ca/ca.pem;
    ssl_verify_client      on;             # handshake faalt zonder geldig client-cert

    location / {
        proxy_pass         http://127.0.0.1:29500/;
        proxy_buffering    off;            # onmisbaar voor SSE
        proxy_read_timeout 1h;
        proxy_set_header   X-SI-Client-Fingerprint $ssl_client_fingerprint;
    }
}
```

> **Niet op je bestaande 443-vhost zetten.** `ssl_verify_client` geldt per server-blok; zet
> je het aan op de vhost die ook je gewone site serveert, dan krijgt elke browserbezoeker
> een certificaatkiezer te zien. Vandaar een eigen blok op een eigen poort of hostname.

## 10.8 Informatie-lekken dichttimmeren

Naast mTLS zelf:

- **`/v1/health` is niet langer publiek.** Nu lekt die versienummer en uptime aan iedereen.
  Voortaan achter het client-certificaat, met één uitzondering: verzoeken vanaf `127.0.0.1`,
  zodat `install.sh` zijn zelftest kan doen.
- **Geen `Server:`-header**, geen softwarenaam in foutpagina's, geen versienummer buiten de
  geauthenticeerde API.
- **Uniforme, lege foutbodies** op 401/403 — geen onderscheid tussen "verkeerd token" en
  "onbekend apparaat", zodat je er niets uit kunt afleiden.
- **Vaste vertraging van 250 ms** op elke authenticatiefout, plus de rate limits uit
  [03 §3.8](03-api-contract.md).
- **Geen mDNS/Bonjour-advertentie** van de agent. Aantrekkelijk voor auto-discovery in de
  app, maar het is letterlijk je server laten omroepen dat hij er is. Als we auto-discovery
  ooit willen: alleen tijdens een open enrollment-venster.

## 10.9 Aan de app-kant

- Sleutel + certificaat staan in de Keychain met
  `kSecAttrAccessibleWhenUnlockedThisDeviceOnly` — geen iCloud-sync, niet leesbaar met het
  toestel vergrendeld, en door de iOS-sandbox onbereikbaar voor andere apps op de telefoon.
- `URLSession` gebruikt de identiteit via een `URLSessionDelegate` die op
  `NSURLAuthenticationMethodClientCertificate` een `URLCredential(identity:certificates:persistence:)`
  teruggeeft.
- **Te verifiëren in Fase 2 (risico-item):** een sleutel in de *Secure Enclave* is nog
  sterker (niet-exporteerbaar hardware-gebonden), maar het bouwen van een `SecIdentity` uit
  een Secure Enclave-sleutel is historisch onbetrouwbaar. Plan: starten met een
  Keychain-sleutel (werkt zeker), en Secure Enclave in Fase 6 uitproberen als hardening.
  Niet vooraf beloven wat nog niet getest is.

## 10.10 Wat dit kost

| | Zonder mTLS | Met mTLS |
|---|---|---|
| Werk in de agent | — | ~200 regels: CA, CSR-ondertekening, devices.json, dynamische TLS-config |
| Werk in de app | — | ~120 regels: keypair, CSR, enrollment-scherm, cert-delegate |
| Gebruikersstappen | token overtypen of QR scannen | QR scannen |
| Uitrol | 1 commando | 1 commando |

De gebruiker merkt er dus niets van, en het is de enige manier om je eis
("geen informatie naar andere apps") echt waar te maken in plaats van te benaderen.
