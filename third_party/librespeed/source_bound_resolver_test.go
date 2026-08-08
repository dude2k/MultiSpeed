// SPDX-License-Identifier: LGPL-3.0-or-later
package speedtest

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSourceBoundResolverUsesSelectedSourceForUDPAndTCPFallback(t *testing.T) {
	resolverIP := net.ParseIP("127.0.0.1")
	sourceIP := net.ParseIP("127.0.0.2")
	udpListener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: resolverIP})
	if err != nil {
		t.Fatalf("listen UDP DNS fixture: %v", err)
	}
	defer func() { _ = udpListener.Close() }()
	port := udpListener.LocalAddr().(*net.UDPAddr).Port
	tcpListener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: resolverIP, Port: port})
	if err != nil {
		t.Fatalf("listen TCP DNS fixture: %v", err)
	}
	defer func() { _ = tcpListener.Close() }()

	observedSources := make(chan net.IP, 2)
	serverErrors := make(chan error, 2)
	go serveTruncatedUDPResponse(udpListener, observedSources, serverErrors)
	go serveFullTCPResponse(tcpListener, observedSources, serverErrors)

	resolver := newSourceBoundResolver(sourceIP, net.JoinHostPort(resolverIP.String(), fmt.Sprint(port)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addresses, err := resolver.LookupIP(ctx, "ip4", "speed.test")
	if err != nil {
		t.Fatalf("source-bound DNS lookup: %v", err)
	}
	if len(addresses) != 1 || !addresses[0].Equal(net.ParseIP("203.0.113.7")) {
		t.Fatalf("DNS addresses=%v", addresses)
	}
	for protocol := 0; protocol < 2; protocol++ {
		select {
		case err := <-serverErrors:
			t.Fatal(err)
		case observed := <-observedSources:
			if !observed.Equal(sourceIP) {
				t.Fatalf("DNS source=%s, want %s", observed, sourceIP)
			}
		case <-ctx.Done():
			t.Fatalf("wait for DNS source observation: %v", ctx.Err())
		}
	}
}

func TestCustomServerDestinationGuardPinsAuthorizedEndpoints(t *testing.T) {
	previousRedirectPolicy := http.DefaultClient.CheckRedirect
	t.Cleanup(func() { http.DefaultClient.CheckRedirect = previousRedirectPolicy })
	t.Setenv(allowedServerEndpointsEnvironment, "192.0.2.10:443,[::ffff:192.0.2.11]:8443")
	dialer := &net.Dialer{}
	if err := restrictDialerToAllowedServerEndpoints(dialer); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"192.0.2.10:443", "192.0.2.11:8443"} {
		if err := dialer.ControlContext(context.Background(), "tcp", address, nil); err != nil {
			t.Fatalf("authorized destination %q rejected: %v", address, err)
		}
	}
	for _, address := range []string{"192.0.2.10:80", "192.0.2.11:443", "198.51.100.10:443", "[2001:db8::10]:443", "speed.example.test:443"} {
		if err := dialer.ControlContext(context.Background(), "tcp", address, nil); err == nil {
			t.Fatalf("unauthorized destination %q accepted", address)
		}
	}
}

func TestCustomServerDestinationGuardFailsClosed(t *testing.T) {
	for _, value := range []string{"", "not-an-endpoint", "192.0.2.10", "192.0.2.10:0", "0.0.0.0:443", "[ff02::1]:443", "[fe80::1%eth0]:443"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(allowedServerEndpointsEnvironment, value)
			if err := restrictDialerToAllowedServerEndpoints(&net.Dialer{}); err == nil {
				t.Fatalf("guard %q unexpectedly succeeded", value)
			}
		})
	}
}

func TestCustomServerDestinationGuardBlocksRedirects(t *testing.T) {
	var redirectedRequests atomic.Int32
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedRequests.Add(1)
	}))
	defer redirectTarget.Close()
	redirectSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusTemporaryRedirect)
	}))
	defer redirectSource.Close()

	previousClient := http.DefaultClient
	dialer := &net.Dialer{}
	http.DefaultClient = &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext}}
	t.Cleanup(func() { http.DefaultClient = previousClient })
	t.Setenv(allowedServerEndpointsEnvironment, redirectSource.Listener.Addr().String())
	if err := restrictDialerToAllowedServerEndpoints(dialer); err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Get(redirectSource.URL)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "redirects are not allowed") {
		t.Fatalf("redirect error=%v", err)
	}
	if redirectedRequests.Load() != 0 {
		t.Fatalf("redirect target received %d requests", redirectedRequests.Load())
	}
}

func serveTruncatedUDPResponse(listener *net.UDPConn, observed chan<- net.IP, failures chan<- error) {
	buffer := make([]byte, 4096)
	_ = listener.SetReadDeadline(time.Now().Add(5 * time.Second))
	count, remote, err := listener.ReadFromUDP(buffer)
	if err != nil {
		failures <- fmt.Errorf("read UDP DNS query: %w", err)
		return
	}
	observed <- append(net.IP(nil), remote.IP...)
	response, err := dnsResponse(buffer[:count], true)
	if err == nil {
		_, err = listener.WriteToUDP(response, remote)
	}
	if err != nil {
		failures <- fmt.Errorf("write UDP DNS response: %w", err)
	}
}

func serveFullTCPResponse(listener *net.TCPListener, observed chan<- net.IP, failures chan<- error) {
	_ = listener.SetDeadline(time.Now().Add(5 * time.Second))
	connection, err := listener.AcceptTCP()
	if err != nil {
		failures <- fmt.Errorf("accept TCP DNS query: %w", err)
		return
	}
	defer func() { _ = connection.Close() }()
	observed <- append(net.IP(nil), connection.RemoteAddr().(*net.TCPAddr).IP...)
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	var length [2]byte
	if _, err = io.ReadFull(connection, length[:]); err != nil {
		failures <- fmt.Errorf("read TCP DNS length: %w", err)
		return
	}
	query := make([]byte, binary.BigEndian.Uint16(length[:]))
	if _, err = io.ReadFull(connection, query); err != nil {
		failures <- fmt.Errorf("read TCP DNS query: %w", err)
		return
	}
	response, err := dnsResponse(query, false)
	if err != nil {
		failures <- err
		return
	}
	binary.BigEndian.PutUint16(length[:], uint16(len(response)))
	if _, err = connection.Write(append(length[:], response...)); err != nil {
		failures <- fmt.Errorf("write TCP DNS response: %w", err)
	}
}

func dnsResponse(query []byte, truncated bool) ([]byte, error) {
	questionEnd, err := dnsQuestionEnd(query)
	if err != nil {
		return nil, err
	}
	response := make([]byte, 12, 12+questionEnd-12+16)
	copy(response[:2], query[:2])
	response[2], response[3] = 0x81, 0x80
	copy(response[4:6], query[4:6])
	response = append(response, query[12:questionEnd]...)
	if truncated {
		response[2] |= 0x02
		return response, nil
	}
	binary.BigEndian.PutUint16(response[6:8], 1)
	response = append(response,
		0xc0, 0x0c, 0x00, 0x01, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x3c, 0x00, 0x04,
		203, 0, 113, 7,
	)
	return response, nil
}

func dnsQuestionEnd(query []byte) (int, error) {
	if len(query) < 17 {
		return 0, fmt.Errorf("short DNS query")
	}
	position := 12
	for {
		if position >= len(query) {
			return 0, fmt.Errorf("invalid DNS question")
		}
		labelLength := int(query[position])
		position++
		if labelLength == 0 {
			break
		}
		position += labelLength
	}
	if position+4 > len(query) {
		return 0, fmt.Errorf("short DNS question")
	}
	return position + 4, nil
}
