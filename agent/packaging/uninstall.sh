#!/usr/bin/env bash
# Verwijdert serverinfo-agent. Werkt ook als de installatie half mislukt is.
set -uo pipefail

BIN=/usr/local/bin/serverinfo-agent
ETC=/etc/serverinfo-agent
STATE=/var/lib/serverinfo-agent
UNIT=/etc/systemd/system/serverinfo-agent.service
SUDOERS=/etc/sudoers.d/serverinfo-agent
USER=serverinfo
PORT=29500

PURGE=0
REMOVE_EXTRAS=0

c()  { printf '\033[%sm%s\033[0m\n' "$1" "$2"; }
ok() { c "0;32" "  ✔ $1"; }
inf(){ printf '  · %s\n' "$1"; }
kept=()

while [ $# -gt 0 ]; do
  case "$1" in
    --purge) PURGE=1; shift;;
    --remove-extras) REMOVE_EXTRAS=1; shift;;
    -h|--help)
      echo "gebruik: sudo ./uninstall.sh [--purge] [--remove-extras]"
      echo "  --purge          verwijder ook config, CA, gekoppelde apparaten en de gebruiker"
      echo "  --remove-extras  verwijder ook smartmontools, whois, dnsutils, qrencode"
      exit 0;;
    *) echo "onbekende optie $1"; exit 2;;
  esac
done

[ "$(id -u)" -eq 0 ] || { c "0;31" "  ✖ draai dit als root"; exit 1; }

echo
c "1" "Server Info agent — verwijderen"
echo

if [ -f "$ETC/config.toml" ]; then
  P="$(grep -E '^bind' "$ETC/config.toml" | sed 's/.*://; s/"//g' | tr -d ' ')"
  [ -n "$P" ] && PORT="$P"
fi

systemctl disable --now serverinfo-agent >/dev/null 2>&1 && ok "service gestopt en uitgeschakeld" || inf "service draaide niet"
[ -f "$UNIT" ] && rm -f "$UNIT" && ok "systemd-unit verwijderd"
systemctl daemon-reload >/dev/null 2>&1
systemctl reset-failed serverinfo-agent >/dev/null 2>&1

[ -f "$BIN" ] && rm -f "$BIN" && ok "binary verwijderd"

if command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q "Server Info"; then
  while ufw status numbered | grep -q "Server Info"; do
    N="$(ufw status numbered | grep -m1 "Server Info" | sed 's/^\[ *\([0-9]*\)\].*/\1/')"
    yes | ufw delete "$N" >/dev/null 2>&1 || break
  done
  ok "ufw-regels verwijderd"
fi

if [ "$PURGE" -eq 1 ]; then
  [ -d "$ETC" ]   && rm -rf "$ETC"   && ok "configuratie, CA en certificaten verwijderd"
  [ -d "$STATE" ] && rm -rf "$STATE" && ok "gekoppelde apparaten verwijderd"
  [ -f "$SUDOERS" ] && rm -f "$SUDOERS" && ok "sudoers-regel verwijderd"
  id -u "$USER" >/dev/null 2>&1 && userdel "$USER" >/dev/null 2>&1 && ok "gebruiker $USER verwijderd"
else
  [ -d "$ETC" ]   && kept+=("$ETC (config, CA, certificaten)")
  [ -d "$STATE" ] && kept+=("$STATE (gekoppelde apparaten)")
  [ -f "$SUDOERS" ] && kept+=("$SUDOERS (sudoers-regel voor smartctl)")
  id -u "$USER" >/dev/null 2>&1 && kept+=("gebruiker $USER")
fi

if [ "$REMOVE_EXTRAS" -eq 1 ]; then
  DEBIAN_FRONTEND=noninteractive apt-get remove -y -qq smartmontools whois dnsutils qrencode lm-sensors >/dev/null 2>&1 \
    && ok "extra pakketten verwijderd" || inf "extra pakketten niet verwijderd"
else
  kept+=("extra pakketten (smartmontools, whois, dnsutils, qrencode, lm-sensors)")
fi

echo
if [ ${#kept[@]} -gt 0 ]; then
  c "0;33" "  Niet verwijderd:"
  for k in "${kept[@]}"; do echo "    · $k"; done
  echo "    Draai met --purge --remove-extras om ook deze op te ruimen."
else
  c "0;32" "  Alles opgeruimd — er is niets van Server Info achtergebleven."
fi
echo
