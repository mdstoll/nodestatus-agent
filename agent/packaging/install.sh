#!/usr/bin/env bash
# Installs the Node Status agent. Idempotent: safe to run again.
#
#   curl -fsSL https://raw.githubusercontent.com/mdstoll/node-status/main/install.sh | sudo bash
#
# or, from an unpacked release tarball:
#
#   sudo ./install.sh --with-extras
set -euo pipefail

REPO=mdstoll/node-status
BIN=/usr/local/bin/nodestatus-agent
ETC=/etc/nodestatus-agent
STATE=/var/lib/nodestatus-agent
UNIT=/etc/systemd/system/nodestatus-agent.service
SUDOERS=/etc/sudoers.d/nodestatus-agent
USER=nodestatus

MODE=lan
PORT=29500
NAME=""
VPN_IP=""
EXTRAS=0
VERSION=latest

c()   { printf '\033[%sm%s\033[0m\n' "$1" "$2"; }
ok()  { c "0;32" "  ✔ $1"; }
inf() { printf '  · %s\n' "$1"; }
warn(){ c "0;33" "  ! $1"; }
die() { c "0;31" "  ✖ $1"; exit 1; }

usage() {
  cat <<'USAGE'
usage: sudo ./install.sh [options]

  --mode lan|vpn|public|proxy   connection profile (default: lan)
  --port <n>                    listen port (default: 29500)
  --name <name>                 display name shown in the app
  --vpn-ip <ip>                 with --mode vpn: address to bind to
  --with-extras                 also install smartmontools, whois, dnsutils,
                                qrencode, lm-sensors and intel-gpu-tools
  --version <tag>               release to install (default: latest)
  --yes                         do not ask anything
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    --mode) MODE="$2"; shift 2;;
    --port) PORT="$2"; shift 2;;
    --name) NAME="$2"; shift 2;;
    --vpn-ip) VPN_IP="$2"; shift 2;;
    --with-extras) EXTRAS=1; shift;;
    --version) VERSION="$2"; shift 2;;
    --yes|-y) shift;;
    -h|--help) usage; exit 0;;
    *) die "unknown option $1 (--help for usage)";;
  esac
done

echo
c "1" "Node Status agent — install"
echo

# ---------- 1. preflight ----------
[ "$(id -u)" -eq 0 ] || die "run this as root (sudo ./install.sh)"
command -v systemctl >/dev/null || die "systemd is required"
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64;;
  aarch64|arm64) ARCH=arm64;;
  *) die "unsupported architecture $(uname -m)";;
esac
ok "preflight ($ARCH, $(. /etc/os-release && echo "$PRETTY_NAME"))"

# ---------- 2. locate or download the binary ----------
# Only look for files next to the script when there *is* a script. Piped from
# curl, BASH_SOURCE is unset; falling back to a directory like /tmp would let
# the installer pick up unrelated leftovers, which is exactly what happened.
SRC_DIR=""
if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ]; then
  SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi
SRC_BIN=""
UNIT_SRC=""
if [ -n "$SRC_DIR" ]; then
  for cand in "$SRC_DIR/nodestatus-agent" "$SRC_DIR/$ARCH/nodestatus-agent"; do
    [ -f "$cand" ] && SRC_BIN="$cand" && break
  done
  [ -n "$SRC_BIN" ] && UNIT_SRC="$(dirname "$SRC_BIN")/nodestatus-agent.service"
fi

if [ -z "$SRC_BIN" ]; then
  # Piped straight from curl: fetch the release ourselves.
  command -v curl >/dev/null || die "curl is required to download the release"
  TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
  if [ "$VERSION" = latest ]; then
    BASEURL="https://github.com/$REPO/releases/latest/download"
  else
    BASEURL="https://github.com/$REPO/releases/download/$VERSION"
  fi
  TARBALL="nodestatus-agent_linux_${ARCH}.tar.gz"
  inf "downloading $TARBALL"
  curl -fsSL -o "$TMP/$TARBALL" "$BASEURL/$TARBALL" \
    || die "download failed — check https://github.com/$REPO/releases"
  if curl -fsSL -o "$TMP/SHA256SUMS" "$BASEURL/SHA256SUMS" 2>/dev/null; then
    ( cd "$TMP" && grep " $TARBALL\$" SHA256SUMS | sha256sum -c --status - ) \
      || die "checksum mismatch — refusing to install"
    ok "checksum verified"
  else
    warn "no SHA256SUMS published; skipping checksum verification"
  fi
  tar xzf "$TMP/$TARBALL" -C "$TMP"
  SRC_BIN="$TMP/nodestatus-agent"
  UNIT_SRC="$TMP/nodestatus-agent.service"
  [ -f "$SRC_BIN" ] || die "release tarball did not contain the binary"
fi

if ss -lnt 2>/dev/null | awk '{print $4}' | grep -q ":$PORT\$"; then
  systemctl is-active --quiet nodestatus-agent || die "port $PORT is already in use by something else"
fi

# ---------- 3. user ----------
if ! id -u "$USER" >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "$USER"
  ok "created system user $USER"
else
  inf "system user $USER already exists"
fi
# Group "disk" lets smartctl identify the drive (model, size) even when the
# SMART log itself needs sudo.
getent group disk >/dev/null && usermod -aG disk "$USER" 2>/dev/null || true

# ---------- 4. binary ----------
systemctl stop nodestatus-agent 2>/dev/null || true
install -m 0755 -o root -g root "$SRC_BIN" "$BIN"
ok "installed binary ($BIN, $(du -h "$BIN" | cut -f1))"

# Ship the uninstaller too, so removing the agent does not depend on still
# having the tarball around.
UNINST_SRC=""
for cand in "$SRC_DIR/uninstall.sh" "$(dirname "$SRC_BIN")/uninstall.sh"; do
  [ -f "$cand" ] && UNINST_SRC="$cand" && break
done
if [ -n "$UNINST_SRC" ]; then
  install -m 0755 "$UNINST_SRC" /usr/local/bin/nodestatus-uninstall
  ln -sf /usr/local/bin/nodestatus-uninstall /usr/local/bin/uninstall.sh
  ok "uninstaller installed (/usr/local/bin/nodestatus-uninstall)"
fi

install -d -m 0750 -o "$USER" -g "$USER" "$ETC" "$ETC/ca" "$STATE"

# ---------- 5. connection profile ----------
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
    [ -n "$VPN_IP" ] || die "--mode vpn needs --vpn-ip <address>"
    BIND="$VPN_IP:$PORT"
    ;;
  public) ;;
  proxy) BIND="127.0.0.1:$PORT"; TLS_CERT="";;
  *) die "unknown profile $MODE";;
esac
[ -n "$NAME" ] || NAME="$(hostname)"

# ---------- 6. configuration ----------
if [ -f "$ETC/config.toml" ]; then
  inf "keeping existing config ($ETC/config.toml)"
else
  cat > "$ETC/config.toml" <<CFG
# Generated by install.sh on $(date -Iseconds)
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
units     = ["ssh", "sshd", "nginx", "apache2", "docker", "containerd", "cron", "ufw", "systemd-journald", "systemd-resolved", "systemd-networkd", "systemd-timesyncd", "NetworkManager", "unattended-upgrades", "fail2ban", "postfix", "smartd"]
files     = ["/var/log/syslog", "/var/log/auth.log", "/var/log/kern.log", "/var/log/messages", "/var/log/daemon.log", "/var/log/dpkg.log", "/var/log/boot.log", "/var/log/ufw.log"]
max_lines = 500
CFG
  chmod 600 "$ETC/config.toml"; chown "$USER:$USER" "$ETC/config.toml"
  ok "wrote configuration (profile: $MODE, bind: $BIND)"
fi

# ---------- 7. optional packages ----------
if [ "$EXTRAS" -eq 1 ]; then
  export DEBIAN_FRONTEND=noninteractive
  inf "installing optional packages…"
  apt-get update -qq || warn "apt-get update failed; continuing with what is present"
  apt-get install -y -qq smartmontools whois dnsutils iputils-ping traceroute \
      qrencode lm-sensors intel-gpu-tools >/dev/null 2>&1 \
    && ok "smartmontools, whois, dnsutils, ping, traceroute, qrencode, lm-sensors, intel-gpu-tools" \
    || warn "not every optional package could be installed"
  if ! command -v speedtest >/dev/null && ! command -v librespeed-cli >/dev/null; then
    warn "no speedtest tool found — install the Ookla CLI or librespeed-cli for the speed test"
  fi
else
  inf "optional packages skipped (--with-extras installs them)"
fi

# ---------- 8. sudoers ----------
# SMART on NVMe needs CAP_SYS_ADMIN. One pinned command through sudo is a far
# narrower privilege than granting the agent that capability outright.
if command -v smartctl >/dev/null; then
  SMARTCTL="$(command -v smartctl)"
  cat > "$SUDOERS" <<SUDO
# Read SMART data only — no other privileges for $USER.
$USER ALL=(root) NOPASSWD: $SMARTCTL -j -A -H -i /dev/sd[a-z], $SMARTCTL -j -A -H -i /dev/nvme[0-9]n[0-9], $SMARTCTL -j -A -H -i /dev/vd[a-z]
SUDO
  chmod 440 "$SUDOERS"
  visudo -cf "$SUDOERS" >/dev/null 2>&1 && ok "sudoers rule for smartctl installed" \
    || { rm -f "$SUDOERS"; warn "sudoers rule invalid, skipped (SMART stays empty)"; }
fi
# intel_gpu_top needs CAP_PERFMON, same trade-off.
if command -v intel_gpu_top >/dev/null; then
  IGT="$(command -v intel_gpu_top)"
  echo "$USER ALL=(root) NOPASSWD: $IGT -J -s 600" > "${SUDOERS}-gpu"
  chmod 440 "${SUDOERS}-gpu"
  visudo -cf "${SUDOERS}-gpu" >/dev/null 2>&1 && ok "sudoers rule for intel_gpu_top installed" \
    || { rm -f "${SUDOERS}-gpu"; warn "GPU sudoers rule invalid, skipped"; }
fi

# ---------- 9. CA + server certificate ----------
# Done here and not in the daemon: ProtectSystem=strict makes /etc read-only
# for the running agent.
BOOTSTRAP_OUT="$(su -s /bin/sh "$USER" -c "$BIN bootstrap --config $ETC/config.toml" 2>&1 || true)"
FP="$(printf '%s' "$BOOTSTRAP_OUT" | awk '/CA-fingerprint/{print $2}')"
[ -n "$FP" ] || die "could not create the CA: $(printf '%s' "$BOOTSTRAP_OUT" | tail -2 | tr '\n' ' ')"
chown -R "$USER:$USER" "$ETC"
ok "CA and server certificate created"

# ---------- 10. systemd ----------
[ -f "$UNIT_SRC" ] || die "nodestatus-agent.service not found next to the binary"
install -m 0644 "$UNIT_SRC" "$UNIT"
systemctl daemon-reload
systemctl enable --quiet nodestatus-agent
systemctl restart nodestatus-agent
sleep 1.5
systemctl is-active --quiet nodestatus-agent || {
  journalctl -u nodestatus-agent -n 20 --no-pager
  die "the agent does not start — see the journal lines above"
}
ok "service active ($(systemctl show -p MainPID --value nodestatus-agent) · $(systemctl show -p MemoryCurrent --value nodestatus-agent | awk '{printf "%.1f MB", $1/1048576}'))"

# ---------- 11. firewall ----------
if command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q "^Status: active"; then
  case "$MODE" in
    lan)    [ -n "${CIDR:-}" ] && ufw allow from "$CIDR" to any port "$PORT" proto tcp comment 'Node Status' >/dev/null && ok "ufw: allowed from $CIDR";;
    public) ufw allow "$PORT"/tcp comment 'Node Status' >/dev/null && ok "ufw: port $PORT open";;
    vpn)    inf "ufw: no rule needed (agent binds the VPN address only)";;
    proxy)  inf "ufw: no rule needed (agent binds loopback)";;
  esac
fi

# ---------- 12. self-test ----------
if [ -n "$TLS_CERT" ]; then
  HEALTH="$(curl -sk --max-time 5 "https://127.0.0.1:$PORT/v1/health" || true)"
else
  HEALTH="$(curl -s  --max-time 5 "http://127.0.0.1:$PORT/v1/health" || true)"
fi
echo "$HEALTH" | grep -q '"ok":true' || { journalctl -u nodestatus-agent -n 20 --no-pager; die "self-test failed"; }
ok "self-test passed"

# ---------- 13. pairing ----------
echo
if [ "$(su -s /bin/sh "$USER" -c "$BIN devices list" 2>/dev/null | grep -c '^d_')" -gt 0 ]; then
  c "1" "  Devices are already paired."
  echo "  To pair another one:  sudo nodestatus-agent enroll --new"
  echo
else
  "$BIN" enroll --new --port "$PORT" || warn "could not open a pairing window; try: sudo nodestatus-agent enroll --new"
fi
c "0;32" "  Done. Manage with: systemctl status nodestatus-agent"
echo
