#!/usr/bin/env bash
# Installeert serverinfo-agent. Idempotent: veilig om opnieuw te draaien.
set -euo pipefail

BIN=/usr/local/bin/serverinfo-agent
ETC=/etc/serverinfo-agent
STATE=/var/lib/serverinfo-agent
UNIT=/etc/systemd/system/serverinfo-agent.service
SUDOERS=/etc/sudoers.d/serverinfo-agent
USER=serverinfo

MODE=lan
PORT=29500
NAME=""
VPN_IP=""
EXTRAS=0
ASSUME_YES=0

c()  { printf '\033[%sm%s\033[0m\n' "$1" "$2"; }
ok() { c "0;32" "  ✔ $1"; }
inf(){ printf '  · %s\n' "$1"; }
warn(){ c "0;33" "  ! $1"; }
die(){ c "0;31" "  ✖ $1"; exit 1; }

usage() {
  cat <<'USAGE'
gebruik: sudo ./install.sh [opties]

  --mode lan|vpn|public|proxy   connectieprofiel (standaard: lan)
  --port <n>                    luisterpoort (standaard: 29500)
  --name <naam>                 weergavenaam in de app
  --vpn-ip <ip>                 alleen bij --mode vpn: adres om op te binden
  --with-extras                 installeer smartmontools, whois, dnsutils, qrencode, speedtest
  --yes                         geen vragen stellen
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    --mode) MODE="$2"; shift 2;;
    --port) PORT="$2"; shift 2;;
    --name) NAME="$2"; shift 2;;
    --vpn-ip) VPN_IP="$2"; shift 2;;
    --with-extras) EXTRAS=1; shift;;
    --yes|-y) ASSUME_YES=1; shift;;
    -h|--help) usage; exit 0;;
    *) die "onbekende optie $1 (--help voor uitleg)";;
  esac
done

echo
c "1" "Server Info agent — installatie"
echo

# ---------- 1. preflight ----------
[ "$(id -u)" -eq 0 ] || die "draai dit script als root (sudo ./install.sh)"
command -v systemctl >/dev/null || die "systemd is vereist"
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64;;
  aarch64|arm64) ARCH=arm64;;
  *) die "niet-ondersteunde architectuur $(uname -m)";;
esac
SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_BIN="$SRC_DIR/serverinfo-agent"
[ -f "$SRC_BIN" ] || SRC_BIN="$SRC_DIR/$ARCH/serverinfo-agent"
[ -f "$SRC_BIN" ] || die "binary niet gevonden naast dit script"

if ss -lnt 2>/dev/null | awk '{print $4}' | grep -q ":$PORT\$"; then
  systemctl is-active --quiet serverinfo-agent || die "poort $PORT is al in gebruik door iets anders"
fi
ok "preflight ($ARCH, $(. /etc/os-release && echo "$PRETTY_NAME"))"

# ---------- 2. gebruiker ----------
if ! id -u "$USER" >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "$USER"
  ok "gebruiker $USER aangemaakt"
else
  inf "gebruiker $USER bestaat al"
fi
# Groep disk: laat smartctl de schijf identificeren (model, grootte) ook als
# de SMART-logs zelf via sudo moeten.
getent group disk >/dev/null && usermod -aG disk "$USER" 2>/dev/null || true

# ---------- 3. binary ----------
systemctl stop serverinfo-agent 2>/dev/null || true
install -m 0755 -o root -g root "$SRC_BIN" "$BIN"
ok "binary geplaatst ($BIN, $(du -h "$BIN" | cut -f1))"

# ---------- 4. mappen ----------
install -d -m 0750 -o "$USER" -g "$USER" "$ETC" "$ETC/ca" "$STATE"

# ---------- 5. netwerkprofiel ----------
detect_cidr() {
  ip -o -f inet addr show 2>/dev/null \
    | awk '$2 !~ /^(lo|docker|veth|br-|virbr|tun|tap|wg)/ {print $4; exit}' \
    | awk -F/ '{split($1,a,"."); print a[1]"."a[2]"."a[3]".0/"$2}'
}
BIND="0.0.0.0:$PORT"; ALLOW="[]"; TLS_CERT="$ETC/cert.pem"
case "$MODE" in
  lan)
    CIDR="$(detect_cidr || true)"
    [ -n "$CIDR" ] && ALLOW="[\"$CIDR\"]"
    ;;
  vpn)
    [ -n "$VPN_IP" ] || die "--mode vpn vereist --vpn-ip <adres>"
    BIND="$VPN_IP:$PORT"
    ;;
  public) ;;
  proxy)
    BIND="127.0.0.1:$PORT"; TLS_CERT=""
    ;;
  *) die "onbekend profiel $MODE";;
esac
[ -n "$NAME" ] || NAME="$(hostname)"

# ---------- 6. configuratie ----------
if [ -f "$ETC/config.toml" ]; then
  inf "bestaande config blijft staan ($ETC/config.toml)"
else
  cat > "$ETC/config.toml" <<CFG
# Gegenereerd door install.sh op $(date -Iseconds)
bind          = "$BIND"
mode          = "$MODE"
display_name  = "$NAME"
tls_cert      = "$TLS_CERT"
tls_key       = "$([ -n "$TLS_CERT" ] && echo "$ETC/key.pem")"
ca_dir        = "$ETC/ca"
state_dir     = "$STATE"
allow_cidr    = $ALLOW
trusted_proxy = $([ "$MODE" = proxy ] && echo '["127.0.0.1"]' || echo '[]')

sample_hz             = 1
history_size          = 300
enroll_window_minutes = 15
enroll_max_attempts   = 5
client_cert_days      = 365

[features]
smart     = true
gpu       = true
speedtest = true
apt       = true
logs      = true

[logs]
units     = ["ssh", "sshd", "nginx", "docker", "cron", "ufw", "systemd-journald", "systemd-resolved"]
files     = ["/var/log/syslog", "/var/log/auth.log", "/var/log/kern.log", "/var/log/daemon.log"]
max_lines = 500
CFG
  chmod 600 "$ETC/config.toml"; chown "$USER:$USER" "$ETC/config.toml"
  ok "configuratie aangemaakt (profiel: $MODE, bind: $BIND)"
fi

# ---------- 7. extra pakketten ----------
if [ "$EXTRAS" -eq 1 ]; then
  export DEBIAN_FRONTEND=noninteractive
  inf "extra pakketten installeren…"
  apt-get update -qq || warn "apt-get update mislukte, ga door met wat er is"
  apt-get install -y -qq smartmontools whois dnsutils iputils-ping traceroute qrencode lm-sensors >/dev/null 2>&1 \
    && ok "smartmontools, whois, dnsutils, ping, traceroute, qrencode, lm-sensors" \
    || warn "niet alle extra pakketten konden worden geïnstalleerd"
  if ! command -v speedtest >/dev/null && ! command -v librespeed-cli >/dev/null; then
    warn "geen speedtest-tool gevonden — installeer de Ookla CLI of librespeed-cli voor de snelheidstest"
  fi
else
  inf "extra pakketten overgeslagen (--with-extras om ze te installeren)"
fi

# ---------- 8. sudoers voor SMART ----------
if command -v smartctl >/dev/null; then
  SMARTCTL="$(command -v smartctl)"
  cat > "$SUDOERS" <<SUDO
# Alleen SMART uitlezen — geen andere rechten voor serverinfo.
$USER ALL=(root) NOPASSWD: $SMARTCTL -j -A -H -i /dev/sd[a-z], $SMARTCTL -j -A -H -i /dev/nvme[0-9]n[0-9], $SMARTCTL -j -A -H -i /dev/vd[a-z]
SUDO
  chmod 440 "$SUDOERS"
  if visudo -cf "$SUDOERS" >/dev/null 2>&1; then
    ok "sudoers-regel voor smartctl geplaatst"
  else
    rm -f "$SUDOERS"; warn "sudoers-regel ongeldig, overgeslagen (SMART blijft dan leeg)"
  fi
fi

# ---------- 9. CA + servercertificaat ----------
# Bewust hier en niet in de daemon: dankzij ProtectSystem=strict is /etc voor
# de draaiende agent read-only.
FP="$(su -s /bin/sh "$USER" -c "$BIN bootstrap --config $ETC/config.toml" 2>&1 | awk '/CA-fingerprint/{print $2}')"
[ -n "$FP" ] || die "CA aanmaken mislukt"
chown -R "$USER:$USER" "$ETC"
ok "CA en servercertificaat aangemaakt"

# ---------- 10. systemd ----------
if [ -f "$SRC_DIR/serverinfo-agent.service" ]; then
  install -m 0644 "$SRC_DIR/serverinfo-agent.service" "$UNIT"
else
  die "serverinfo-agent.service niet gevonden naast dit script"
fi
systemctl daemon-reload
systemctl enable --quiet serverinfo-agent
systemctl restart serverinfo-agent
sleep 1.5
systemctl is-active --quiet serverinfo-agent || {
  journalctl -u serverinfo-agent -n 20 --no-pager
  die "de agent start niet — zie de journal-regels hierboven"
}
ok "service actief ($(systemctl show -p MainPID --value serverinfo-agent) · $(systemctl show -p MemoryCurrent --value serverinfo-agent | awk '{printf "%.1f MB", $1/1048576}'))"

# ---------- 11. firewall ----------
if command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q "^Status: active"; then
  case "$MODE" in
    lan)    [ -n "${CIDR:-}" ] && ufw allow from "$CIDR" to any port "$PORT" proto tcp comment 'Server Info' >/dev/null && ok "ufw: toegestaan vanaf $CIDR";;
    public) ufw allow "$PORT"/tcp comment 'Server Info' >/dev/null && ok "ufw: poort $PORT open";;
    vpn)    inf "ufw: geen regel nodig (agent luistert alleen op het VPN-adres)";;
    proxy)  inf "ufw: geen regel nodig (agent luistert op loopback)";;
  esac
fi

# ---------- 12. zelftest ----------
if [ -n "$TLS_CERT" ]; then
  HEALTH="$(curl -sk --max-time 5 "https://127.0.0.1:$PORT/v1/health" || true)"
else
  HEALTH="$(curl -s  --max-time 5 "http://127.0.0.1:$PORT/v1/health" || true)"
fi
echo "$HEALTH" | grep -q '"ok":true' || { journalctl -u serverinfo-agent -n 20 --no-pager; die "zelftest mislukt"; }
ok "zelftest geslaagd"

# ---------- 13. koppelen ----------
echo
if [ "$(su -s /bin/sh "$USER" -c "$BIN devices list" 2>/dev/null | grep -c '^d_')" -gt 0 ]; then
  c "1" "  Er zijn al apparaten gekoppeld."
  echo "  Nieuw apparaat koppelen:  sudo serverinfo-agent enroll --new"
  echo
else
  "$BIN" enroll --new --port "$PORT" || warn "kon geen koppelvenster openen; probeer: sudo serverinfo-agent enroll --new"
fi
c "0;32" "  Klaar. Beheer met: systemctl status serverinfo-agent"
echo
