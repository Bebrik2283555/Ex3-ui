package job

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/valyala/fasthttp"
)

func stallingListener(t *testing.T) (addr string, release func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				<-done
				_ = c.Close()
			}(conn)
			go func(c net.Conn) { _, _ = io.Copy(io.Discard, c) }(conn)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

func TestExternalInformClient_StalledReceiverTimesOut(t *testing.T) {
	// The production dialer blocks loopback by design; this test needs a local
	// receiver, so it drives the client through a plain dialer.
	restore := externalInformDial
	externalInformDial = (&net.Dialer{}).DialContext
	t.Cleanup(func() { externalInformDial = restore })

	addr, release := stallingListener(t)
	defer release()

	request := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(request)
	request.Header.SetMethod("POST")
	request.Header.SetContentType("application/json; charset=UTF-8")
	request.SetBody([]byte(`{"clientTraffics":[],"inboundTraffics":[]}`))
	request.SetRequestURI("http://" + addr + "/inform")
	request.Header.SetConnectionClose()

	response := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(response)

	start := time.Now()
	err := externalInformClient.DoTimeout(request, response, externalInformTimeout)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a receiver that never answers must produce an error, not a completed request")
	}
	if !errors.Is(err, fasthttp.ErrTimeout) {
		t.Errorf("want a timeout error, got %v", err)
	}
	if elapsed > externalInformTimeout+2*time.Second {
		t.Errorf("call took %v, must be bounded by %v", elapsed, externalInformTimeout)
	}
}

func TestExternalInformClient_HasDeadlines(t *testing.T) {
	if externalInformClient.ReadTimeout <= 0 || externalInformClient.WriteTimeout <= 0 {
		t.Fatal("the traffic-notify client must carry read and write deadlines")
	}
	if externalInformTimeout >= 5*time.Second {
		t.Errorf("the call budget (%v) must stay under the 5s traffic poll cadence", externalInformTimeout)
	}
}

// The URL is validated by SanitizePublicHTTPURL, but the connect must not
// resolve it again: a rebinding host would otherwise reach loopback.
func TestExternalInformClient_BlocksLoopback(t *testing.T) {
	addr, release := stallingListener(t)
	defer release()

	request := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(request)
	request.Header.SetMethod("POST")
	request.SetBody([]byte(`{}`))
	request.SetRequestURI("http://" + addr + "/inform")
	request.Header.SetConnectionClose()

	response := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(response)

	if err := externalInformClient.DoTimeout(request, response, externalInformTimeout); err == nil {
		t.Fatal("the inform client must refuse a loopback address at dial time")
	}
}

func TestInformSkippedWhenNothingToReport(t *testing.T) {
	j := &XrayTrafficJob{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		j.informTrafficToExternalAPI(nil, nil)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("an empty payload must return before touching settings or the network")
	}
}
