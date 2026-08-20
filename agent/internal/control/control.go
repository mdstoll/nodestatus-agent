// Package control biedt een unix-socket zodat de CLI-subcommando's met de
// draaiende agent kunnen praten. Zonder dit zou 'devices revoke' pas na een
// herstart effect hebben.
package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"serverinfo/internal/pki"
	"serverinfo/internal/store"
)

type Request struct {
	Cmd string `json:"cmd"`
	Arg string `json:"arg,omitempty"`
}

type Response struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

type EnrollInfo struct {
	Code        string   `json:"code"`
	ExpiresAt   int64    `json:"expires_at"`
	Fingerprint string   `json:"fingerprint"`
	Hostname    string   `json:"hostname"`
	Addresses   []string `json:"addresses"`
}

func SocketPath(stateDir string) string { return filepath.Join(stateDir, "control.sock") }

type Server struct {
	path   string
	store  *store.Store
	ca     *pki.CA
	window time.Duration
	log    *slog.Logger
	ln     net.Listener
}

func NewServer(stateDir string, st *store.Store, ca *pki.CA, window time.Duration, log *slog.Logger) *Server {
	return &Server{path: SocketPath(stateDir), store: st, ca: ca, window: window, log: log}
}

func (s *Server) Start() error {
	_ = os.Remove(s.path)
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return err
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return err
	}
	s.ln = ln
	go s.accept()
	return nil
}

func (s *Server) Close() {
	if s.ln != nil {
		s.ln.Close()
	}
	_ = os.Remove(s.path)
}

func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return
	}
	var req Request
	if json.Unmarshal(line, &req) != nil {
		writeResp(c, Response{Message: "onleesbaar verzoek"})
		return
	}
	switch req.Cmd {
	case "enroll-new":
		code := s.store.OpenEnrollment(s.window)
		host, _ := os.Hostname()
		writeResp(c, Response{OK: true, Data: EnrollInfo{
			Code: code, ExpiresAt: s.store.EnrollmentExpiry().Unix(),
			Fingerprint: s.ca.Fingerprint(), Hostname: host, Addresses: localAddrs(),
		}})
	case "enroll-cancel":
		s.store.CloseEnrollment()
		writeResp(c, Response{OK: true, Message: "koppelvenster gesloten"})
	case "devices-list":
		writeResp(c, Response{OK: true, Data: s.store.List()})
	case "devices-revoke":
		name, err := s.store.Revoke(req.Arg)
		if err != nil {
			writeResp(c, Response{Message: err.Error()})
			return
		}
		writeResp(c, Response{OK: true, Message: name})
	case "status":
		host, _ := os.Hostname()
		writeResp(c, Response{OK: true, Data: map[string]any{
			"hostname": host, "devices": s.store.Count(),
			"enrollment_open": s.store.EnrollmentOpen(),
			"fingerprint":     s.ca.Fingerprint(),
		}})
	default:
		writeResp(c, Response{Message: "onbekend commando"})
	}
}

func writeResp(c net.Conn, r Response) {
	b, _ := json.Marshal(r)
	c.Write(append(b, '\n'))
}

func localAddrs() []string {
	out := []string{}
	ifs, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtual(i.Name) {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}

func isVirtual(n string) bool {
	for _, p := range []string{"veth", "docker", "br-", "virbr", "tun", "tap", "wg", "cni", "kube", "zt"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// Call stuurt één commando naar de draaiende agent.
func Call(stateDir string, req Request) (*Response, error) {
	c, err := net.DialTimeout("unix", SocketPath(stateDir), 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("kan de agent niet bereiken (draait serverinfo-agent?): %w", err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	b, _ := json.Marshal(req)
	if _, err := c.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
