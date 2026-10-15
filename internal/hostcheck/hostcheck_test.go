package hostcheck

import "testing"

const udpLine = "[2026/10/14 23:20:48   WARN]                  [QUICTransport] UDP receive buffer is 425984 bytes (effective 212992), below the 25165824-byte autotune window. Raise net.core.rmem_max so inbound bursts are not dropped"

func TestUDPBufferWarning(t *testing.T) {
	m := NewMonitor()
	m.Observe("[HytaleServer] Booting up HytaleServer - Version: 0.7.2")
	m.Observe(udpLine)
	m.Observe(udpLine) // repeated for every socket
	w := m.Warnings()
	if len(w) != 1 || w[0].Kind != UDPReceiveBuffer {
		t.Fatalf("warnings %+v", w)
	}
	if v := w[0].Values; v["requested"] != 425984 || v["effective"] != 212992 || v["wanted"] != 25165824 {
		t.Fatalf("values %+v", v)
	}
}

func TestWarningsClearedOnNextBoot(t *testing.T) {
	m := NewMonitor()
	m.Observe(udpLine)
	m.Observe("[HytaleServer] Booting up HytaleServer - Version: 0.7.2")
	if w := m.Warnings(); len(w) != 0 {
		t.Fatalf("warnings kept across boots: %+v", w)
	}
}
