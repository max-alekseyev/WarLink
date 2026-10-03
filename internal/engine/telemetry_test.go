package engine

import (
	"testing"
	"time"
)

func TestTelemetryMonitor_Lifecycle(t *testing.T) {
	tm := NewTelemetryMonitor()
	if tm == nil {
		t.Fatalf("expected non-nil TelemetryMonitor")
	}

	ping, loss := tm.Get()
	if ping != 0 || loss != 0 {
		t.Errorf("expected initial 0 ping and 0 loss, got %d ms, %d%%", ping, loss)
	}

	// Start monitor
	tm.Start(false)
	time.Sleep(200 * time.Millisecond)

	// Stop monitor
	tm.Stop()

	pingAfter, lossAfter := tm.Get()
	if pingAfter != 0 || lossAfter != 0 {
		t.Errorf("expected 0 ping and 0 loss after stop, got %d ms, %d%%", pingAfter, lossAfter)
	}
}

func TestProbeDirect_Localhost(t *testing.T) {
	// Probing an unreachable or closed port should cleanly report false without panicking
	rtt, ok := probeDirect("127.0.0.1:59999", 100*time.Millisecond)
	if ok {
		t.Errorf("expected probe to closed port to fail, got ok=true, rtt=%d", rtt)
	}
}

func TestTelemetryMonitor_GetDetailed(t *testing.T) {
	tm := NewTelemetryMonitor()
	tm.pingMs = 45
	tm.packetLoss = 5
	tm.rtts = []int{40, 50, 42, 48}

	ping, loss, jitter := tm.GetDetailed()
	if ping != 45 || loss != 5 {
		t.Errorf("expected ping=45, loss=5, got %d, %d", ping, loss)
	}
	// |50-40|=10, |42-50|=8, |48-42|=6 -> (10+8+6)/3 = 24/3 = 8
	if jitter != 8 {
		t.Errorf("expected jitter 8, got %d", jitter)
	}
}
