#!/usr/bin/env bash
# Removes the Node Status agent. Works even if the install half-failed.
set -uo pipefail

BIN=/usr/local/bin/nodestatus-agent
ETC=/etc/nodestatus-agent
STATE=/var/lib/nodestatus-agent
UNIT=/etc/systemd/system/nodestatus-agent.service
SUDOERS=/etc/sudoers.d/nodestatus-agent
USER=nodestatus
OPT=/opt/nodestatus-agent
EXTRA_PKGS="smartmontools whois dnsutils traceroute qrencode lm-sensors iperf3 intel-gpu-tools"

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
      echo "usage: sudo nodestatus-uninstall.sh [--purge] [--remove-extras]"
      echo "  --purge          also remove config, CA, paired devices, sudoers rules and the user"
      echo "  --remove-extras  also remove Geekbench and the optional packages"
      echo "                   ($EXTRA_PKGS)"
      exit 0;;
    *) echo "unknown option $1"; exit 2;;
  esac
done

[ "$(id -u)" -eq 0 ] || { c "0;31" "  ✖ run this as root"; exit 1; }

echo
c "1" "Node Status agent — uninstall"
echo

systemctl disable --now nodestatus-agent >/dev/null 2>&1 && ok "service stopped and disabled" || inf "service was not running"
[ -f "$UNIT" ] && rm -f "$UNIT" && ok "systemd unit removed"
# Drop-ins (zoals die voor Geekbench) horen bij deze unit en hebben zonder
# hem geen betekenis meer.
[ -d "$UNIT.d" ] && rm -rf "$UNIT.d" && ok "systemd drop-ins removed"
systemctl daemon-reload >/dev/null 2>&1
systemctl reset-failed nodestatus-agent >/dev/null 2>&1

[ -f "$BIN" ] && rm -f "$BIN" && ok "binary removed"
rm -f /usr/local/bin/nodestatus-uninstall.sh /usr/local/bin/nodestatus-uninstall /usr/local/bin/uninstall.sh 2>/dev/null || true

if command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q "Node Status"; then
  while ufw status numbered | grep -q "Node Status"; do
    N="$(ufw status numbered | grep -m1 "Node Status" | sed 's/^\[ *\([0-9]*\)\].*/\1/')"
    yes | ufw delete "$N" >/dev/null 2>&1 || break
  done
  ok "ufw rules removed"
fi

if [ "$PURGE" -eq 1 ]; then
  [ -d "$ETC" ]   && rm -rf "$ETC"   && ok "config, CA and certificates removed"
  [ -d "$STATE" ] && rm -rf "$STATE" && ok "paired devices removed"
  [ -f "$SUDOERS" ] && rm -f "$SUDOERS" && ok "sudoers rule removed"
  [ -f "${SUDOERS}-gpu" ] && rm -f "${SUDOERS}-gpu" && ok "GPU sudoers rule removed"
  id -u "$USER" >/dev/null 2>&1 && userdel "$USER" >/dev/null 2>&1 && ok "user $USER removed"
else
  [ -d "$ETC" ]   && kept+=("$ETC (config, CA, certificates)")
  [ -d "$STATE" ] && kept+=("$STATE (paired devices)")
  [ -f "$SUDOERS" ] && kept+=("$SUDOERS (sudoers rule for smartctl)")
  [ -f "${SUDOERS}-gpu" ] && kept+=("${SUDOERS}-gpu (sudoers rule for intel_gpu_top)")
  id -u "$USER" >/dev/null 2>&1 && kept+=("user $USER")
fi

# iputils-ping staat bewust niet in EXTRA_PKGS: dat levert het systeem-ping
# en hoort op vrijwel elke machine thuis, ook al zette install.sh het neer.
if [ "$REMOVE_EXTRAS" -eq 1 ]; then
  [ -d "$OPT" ] && rm -rf "$OPT" && ok "Geekbench removed ($OPT)"
  # Per pakket, net als bij installeren: één pakket dat hier niet bestaat
  # (intel-gpu-tools op ARM) liet apt-get anders de hele lijst weigeren.
  removed=0
  for pkg in $EXTRA_PKGS; do
    dpkg -s "$pkg" >/dev/null 2>&1 || continue
    DEBIAN_FRONTEND=noninteractive apt-get remove -y -qq "$pkg" >/dev/null 2>&1 && removed=$((removed+1))
  done
  ok "optional packages removed ($removed)"
else
  [ -d "$OPT" ] && kept+=("$OPT (Geekbench)")
  kept+=("optional packages ($EXTRA_PKGS, where installed)")
fi

echo
if [ ${#kept[@]} -gt 0 ]; then
  c "0;33" "  Left in place:"
  for k in "${kept[@]}"; do echo "    · $k"; done
  echo "    Run with --purge --remove-extras to clean these up too."
else
  c "0;32" "  All clean — nothing of Node Status was left behind."
fi
echo
