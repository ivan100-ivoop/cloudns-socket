package gateway

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ivan100-ivoop/cloudns-socket/provider"
)

var ErrServerClosed = errors.New("gateway: server closed")

type Server struct {
	cfg      Config
	registry *provider.Registry
	logger   *log.Logger
	listener net.Listener
	allowed  []*net.IPNet
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
}

func NewServer(cfg Config, registry *provider.Registry, logger *log.Logger) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if registry == nil {
		return nil, fmt.Errorf("gateway: provider registry is required")
	}
	if logger == nil {
		logger = log.Default()
	}
	allowed, err := parseAllowedIPs(cfg.Server.AllowedIPs)
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, registry: registry, logger: logger, allowed: allowed, conns: make(map[net.Conn]struct{})}, nil
}

func (s *Server) Serve(listener net.Listener) error {
	if listener == nil {
		return fmt.Errorf("gateway: listener is required")
	}
	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, ErrServerClosed) {
				return ErrServerClosed
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Temporary() {
				s.logger.Printf("accept_error=%v", err)
				continue
			}
			return fmt.Errorf("gateway: accept: %w", err)
		}

		s.wg.Add(1)
		s.track(conn)
		go func() {
			defer s.wg.Done()
			defer s.untrack(conn)
			defer conn.Close()
			s.handleConnection(conn)
		}()
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	listener := s.listener
	s.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.closeConnections()
		return ctx.Err()
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	remoteIP := connectionIP(conn)
	if !s.isAllowed(remoteIP) {
		s.logger.Printf("remote=%s access=denied", conn.RemoteAddr())
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(s.deadline(s.cfg.Server.ReadTimeout)))
	request, err := readDomain(conn, s.cfg.Server.MaxRequestSize)
	if err != nil {
		s.logger.Printf("remote=%s domain= result=ERROR error=%v", conn.RemoteAddr(), err)
		s.writeResponse(conn, s.cfg.Server.ErrorResponse)
		return
	}

	parts, err := provider.ParseDomain(request)
	if err != nil {
		s.logger.Printf("remote=%s domain=%s result=ERROR error=invalid_domain", conn.RemoteAddr(), request)
		s.writeResponse(conn, s.cfg.Server.ErrorResponse)
		return
	}
	providerID, extension, err := s.registry.ProviderForDomain(parts.Domain)
	if err != nil {
		s.logger.Printf("remote=%s domain=%s tld=%s provider= result=ERROR error=provider_not_found", conn.RemoteAddr(), parts.Domain, parts.Extension)
		s.writeResponse(conn, s.cfg.Server.ErrorResponse)
		return
	}
	s.logger.Printf("remote=%s domain=%s tld=%s provider=%s", conn.RemoteAddr(), parts.Domain, strings.TrimPrefix(extension, "."), providerID)

	result, err := s.registry.Check(context.Background(), parts.Domain)
	if err != nil {
		s.logger.Printf("domain=%s provider=%s result=ERROR error=%v", parts.Domain, providerID, err)
		s.writeResponse(conn, result.SocketResponse)
		return
	}
	s.logger.Printf("domain=%s provider=%s result=%s", result.Domain, result.ProviderID, result.Result)
	s.writeResponse(conn, result.SocketResponse)
}

func parseAllowedIPs(entries []string) ([]*net.IPNet, error) {
	allowed := make([]*net.IPNet, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			bits := 128
			if ip.To4() != nil {
				ip = ip.To4()
				bits = 32
			}
			allowed = append(allowed, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("gateway: parse allowed IP %q: %w", entry, err)
		}
		allowed = append(allowed, network)
	}
	return allowed, nil
}

func connectionIP(conn net.Conn) net.IP {
	if address, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		return address.IP
	}
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return nil
	}
	return net.ParseIP(host)
}

func (s *Server) isAllowed(ip net.IP) bool {
	if len(s.allowed) == 0 {
		return true
	}
	for _, network := range s.allowed {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func readDomain(conn net.Conn, maxSize int) (string, error) {
	reader := bufio.NewReader(conn)
	data := make([]byte, 0, maxSize)
	for {
		char, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && len(data) > 0 {
				break
			}
			return "", fmt.Errorf("read request: %w", err)
		}
		if char == '\n' {
			break
		}
		data = append(data, char)
		if len(data) > maxSize {
			return "", fmt.Errorf("request exceeds %d bytes", maxSize)
		}
	}
	request := strings.TrimSpace(string(data))
	if request == "" {
		return "", fmt.Errorf("empty request")
	}
	return request, nil
}

func (s *Server) writeResponse(conn net.Conn, response string) {
	_ = conn.SetWriteDeadline(time.Now().Add(s.deadline(s.cfg.Server.WriteTimeout)))
	if strings.TrimSpace(response) == "" {
		response = s.cfg.Server.ErrorResponse
	}
	_, _ = io.WriteString(conn, strings.TrimRight(response, "\r\n")+"\r\n")
}

func (s *Server) deadline(configured time.Duration) time.Duration {
	if s.cfg.Server.IdleTimeout < configured {
		return s.cfg.Server.IdleTimeout
	}
	return configured
}

func (s *Server) track(conn net.Conn) {
	s.mu.Lock()
	s.conns[conn] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) untrack(conn net.Conn) {
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}

func (s *Server) closeConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for conn := range s.conns {
		_ = conn.Close()
	}
}
