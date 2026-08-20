# 06 — Designsysteem

Gebaseerd op je screenshots en op de iOS 26 (Liquid Glass) richtlijnen. Kern van die
richtlijnen die hier direct van toepassing is:

- **Glas is voor de navigatielaag** (tabbar, navbar, knoppen) — niet voor content. Kaarten
  met data blijven ondoorzichtig. Dat is precies wat je screenshots laten zien: een
  zwevende glazen tabbar boven vlakke, donkere kaarten.
- **Tekst staat nooit direct op glas**, altijd op een vaste ondergrond. Contrast ≥ 4,5:1.
- **Hiërarchie door diepte**: belangrijker = meer visueel gewicht, niet meer kleur.

## 6.1 Kleuren

Definieer alles als asset-catalog kleuren zodat light mode gratis meekomt.

| Token | Dark | Gebruik |
|-------|------|---------|
| `bg/base` | `#000000` | Schermachtergrond |
| `bg/card` | `#1C1C1E` | Kaarten (systemGray6-achtig) |
| `bg/cardElevated` | `#2C2C2E` | Genest blok in een kaart |
| `stroke/hairline` | `#FFFFFF` @ 8% | 0,5 pt randje op kaarten |
| `text/primary` | `#FFFFFF` | Waarden, titels |
| `text/secondary` | `#EBEBF5` @ 60% | Labels |
| `text/tertiary` | `#EBEBF5` @ 30% | Assen, hints |
| `accent` | systemBlue | Selectie, links, actieve tab |

**Statuskleuren** (met bewuste drempels, niet ad hoc):

| Status | Kleur | Drempel |
|--------|-------|---------|
| ok | `#30D158` groen | < 70% (temp: < high×0,85) |
| warn | `#FF9F0A` oranje | 70–89% |
| crit | `#FF375F` rood | ≥ 90% (of ≥ critical-drempel) |

## 6.2 Gradients voor de balken

Uit je screenshots afgeleid, als `LinearGradient(.leading → .trailing)`:

| Metriek | Van | Naar |
|---------|-----|------|
| CPU | `#0A84FF` | `#32D0FF` |
| RAM | `#0A84FF` | `#32D0FF` |
| Storage | `#FF2D9B` | `#FF453A` |
| Load / Swap | `#30D158` | `#66E39A` |
| Netwerk ↑ | `#30D158` | — (lijn, geen gradient) |
| Netwerk ↓ | `#0A84FF` | — |

De gradient loopt over de **volle breedte van de balk**, niet over het gevulde deel —
zo verandert de kleur bij 22% niet mee met de vulling. Dat is subtiel en het verschil
tussen "ziet er goed uit" en "ziet er goedkoop uit".

## 6.3 Typografie

| Rol | Stijl |
|-----|-------|
| Schermtitel | `.largeTitle` bold |
| Subtitel | `.subheadline` secundair |
| Kaarttitel | `.headline` |
| Grote waarde | `.title2` bold, **`.monospacedDigit()`** |
| Label | `.subheadline` |
| Waarde-detail | `.footnote` monospaced digit |
| Chart-as | `.caption2` tertiair |

`.monospacedDigit()` is verplicht op alles wat elke seconde verandert — anders "wiebelt"
de layout doordat een `1` smaller is dan een `8`. Dit is de meest voorkomende fout in
real-time dashboards.

## 6.4 Vormen en ruimte

| Element | Waarde |
|---------|--------|
| Kaart-radius | 20 pt (`.rect(cornerRadius: 20, style: .continuous)`) |
| Icoontegel-radius | 10 pt, 32×32 |
| Kaart-padding | 16 pt |
| Ruimte tussen kaarten | 12 pt |
| Ruimte tussen secties | 24 pt |
| Schermmarge | 16 pt |
| Balk-hoogte | 8 pt, radius 4, capsule-uiteinden |
| Grid | 2 kolommen, `LazyVGrid` met `.adaptive(minimum: 160)` |

## 6.5 Iconenset (SF Symbols, één consistente familie)

Gebruik overal `.fill`-varianten in gekleurde tegels, exact zoals je screenshots.

| Concept | Symbol | Tegelkleur |
|---------|--------|-----------|
| Server | `server.rack` | indigo |
| CPU | `cpu.fill` | blauw |
| RAM | `memorychip.fill` | blauw |
| Storage | `internaldrive.fill` | magenta |
| Netwerk | `wifi` / `cable.connector` | groen |
| Temperatuur | `thermometer.medium` | groen→oranje→rood naar status |
| GPU | `cpu.fill` in paars, of `display` | paars |
| Sensors | `sensor.fill` | teal |
| Uptime | `clock.fill` | grijs |
| Updates | `arrow.down.circle.fill` | oranje |
| Logs | `doc.text.magnifyingglass` | blauw |
| Speedtest | `speedometer` | cyaan |
| Ping | `dot.radiowaves.left.and.right` | blauw |
| DNS | `globe` | blauw |
| WHOIS | `magnifyingglass.circle.fill` | oranje |
| Locale | `globe.europe.africa.fill` | blauw |
| Settings | `gearshape.fill` | grijs |

**Tab-iconen** (dun, niet-`.fill` — dat is de conventie voor tabbars):
`server.rack` · `chart.bar.xaxis` · `wrench.and.screwdriver` · `gearshape`

## 6.6 Kerncomponenten

### `MetricCard`
```
┌────────────────────────────┐
│ ▢ CPU                      │   icoontegel 32×32 + titel
│                            │
│ ████████░░░░░░░░░░  64.4%  │   GaugeBar + waarde rechts, monospaced
│ 6.7 G / 7.5 G              │   optionele subtekst links
└────────────────────────────┘
```

### `GaugeBar`
Achtergrond `Capsule().fill(white 12%)`, voorgrond `Capsule().fill(gradient)` met
`.frame(width: proxy.size.width * fraction)`. Animatie `.easeOut(0.35)`.
Minimumbreedte 6 pt zodat 0,3% nog zichtbaar is als een stipje in plaats van niets.

### Meerdere opslagvolumes — de oplossing voor jouw vraag
Aanbevolen: **gesegmenteerde balk + uitklaplijst**.

```
┌────────────────────────────────────────┐
│ ▢ Storage                    63.9%     │
│ ███████│█████░░░░░░░░░░░░░░            │  segmenten per volume, elk een tint
│ 565.1 G / 2.0 T          3 volumes  ⌄  │
│ ────────────────────────────────────── │  (uitgeklapt)
│  /       ███████░░░░░  63.9%  315/494G │
│  /data   ███░░░░░░░░░  26.7%  402/1.5T │
│  /boot   █████████░░░  71.2%   0.7/1G  │
└────────────────────────────────────────┘
```

De segmenten van de samenvattende balk gebruiken tinten van hetzelfde magenta→rood-verloop
met 1 pt zwarte scheiding ertussen, zodat het één balk blijft maar de verdeling zichtbaar
is. Bij één volume valt de segmentering vanzelf weg en is het exact je screenshot.
Bij meer dan vijf volumes: toon de vier grootste + "3 overige" samengevoegd.

### `NetworkChart`
Swift Charts, `AreaMark` (opacity 0,25, gradient naar transparant) onder `LineMark`
(2 pt, `.round` linecap, `.interpolationMethod(.monotone)`) per richting.
Gridlijnen: `.chartYAxis { AxisMarks { AxisGridLine(stroke: .init(dash: [4,4])) } }`
in tertiaire kleur, labels links binnen de chart. X-as zonder labels, drie verticale
stippellijnen. Huidige waarden rechtsboven als overlay, groen ↑ en blauw ↓.

### `SensorTile`
64 pt cirkel met de statuskleur op 15% opacity als vulling, icoon in de statuskleur,
rechtsonder een kleine badge (`checkmark.circle.fill` groen / `xmark.circle.fill` rood),
naam eronder in `.caption`. Exact het patroon uit je derde screenshot.

### `ToolRow`
`Label` met gekleurde icoontegel links, titel, chevron rechts, in een
`.listStyle(.insetGrouped)` met `bg/card` — het patroon uit je Tools-screenshot.

## 6.7 Bewegingsprincipes

| Wat | Duur / curve |
|-----|--------------|
| Balkvulling | 0,35 s easeOut |
| Getalveranderingen | `.contentTransition(.numericText())` |
| Nieuw chart-punt | geen aparte animatie — de hele serie schuift met 1,0 s linear |
| Sectie in/uitklappen | `.spring(response: 0.35, dampingFraction: 0.85)` |
| Tabwissel | standaard iOS |
| Statuskleur-omslag | 0,6 s easeInOut (voorkomt knipperen op een drempel) |

Alles respecteert `Reduce Motion`.

## 6.8 Light mode

Niet leidend, maar wel correct: `bg/base` → `#F2F2F7`, `bg/card` → `#FFFFFF`,
gradients blijven identiek, `stroke/hairline` → zwart 8%, tekstkleuren omdraaien.
Omdat alles via asset-tokens loopt, is dit nul extra werk in de views.
