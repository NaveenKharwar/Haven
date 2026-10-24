package main

import "testing"

func TestParseIPNeigh(t *testing.T) {
	out := `192.168.1.1 dev eth0 lladdr aa:bb:cc:dd:ee:01 REACHABLE
192.168.1.20 dev eth0 lladdr 3c:22:fb:11:22:33 STALE
192.168.1.30 dev eth0  FAILED
192.168.1.40 dev eth0 lladdr 00:00:00:00:00:00 STALE
172.17.0.2 dev docker0 lladdr 02:42:ac:11:00:02 REACHABLE`
	got := parseIPNeigh(out)
	if len(got) != 3 || got[0].MAC != "aa:bb:cc:dd:ee:01" || got[1].IP != "192.168.1.20" || got[2].If != "docker0" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseArpMac(t *testing.T) {
	out := `? (192.168.1.1) at a:b:c:d:e:1 on en0 ifscope [ethernet]
? (192.168.1.255) at ff:ff:ff:ff:ff:ff on en0 ifscope [ethernet]
? (192.168.1.9) at (incomplete) on en0 ifscope [ethernet]`
	got := parseArp(out)
	if len(got) != 1 || got[0].MAC != "0a:0b:0c:0d:0e:01" {
		t.Fatalf("got %+v", got)
	}
}

func TestRandomMAC(t *testing.T) {
	for mac, want := range map[string]bool{"da:a1:19:00:00:01": true, "3c:22:fb:11:22:33": false, "02:42:ac:11:00:02": true, "ae:00:00:00:00:00": true, "a8:00:00:00:00:00": false} {
		if isRandomMAC(mac) != want {
			t.Errorf("%s: want %v", mac, want)
		}
	}
}

func TestParseDefaultRoute(t *testing.T) {
	tbl := "Iface\tDestination\tGateway\tFlags\nwlp2s0\t00000000\t0101A8C0\t0003\nwlp2s0\t0001A8C0\t00000000\t0001\n"
	gw, ifc := parseDefaultRoute(tbl)
	if gw != "192.168.1.1" || ifc != "wlp2s0" {
		t.Fatalf("%s %s", gw, ifc)
	}
}

func TestParseSS(t *testing.T) {
	out := `tcp LISTEN 0 4096 0.0.0.0:8484 0.0.0.0:* users:(("haven",pid=812,fd=7))
tcp LISTEN 0 4096 [::]:8484 [::]:* users:(("haven",pid=812,fd=8))
tcp LISTEN 0 128 127.0.0.1:5432 0.0.0.0:* users:(("postgres",pid=900,fd=5))
udp UNCONN 0 0 0.0.0.0:5353 0.0.0.0:*`
	p := parseSS(out)
	if len(p) != 3 {
		t.Fatalf("got %+v", p)
	}
	if p[1].Scope != "network" || p[1].Process != "haven" || p[2].Scope != "this server only" {
		t.Fatalf("got %+v", p)
	}
}

func TestParseLsblkAndGPU(t *testing.T) {
	d := parseLsblk(`{"blockdevices":[{"name":"nvme0n1","model":"Samsung 970","size":500107862016,"type":"disk","rota":false,"tran":"nvme"},{"name":"sda","model":"WD Blue","size":1000204886016,"type":"disk","rota":true,"tran":"sata"},{"name":"loop0","size":1,"type":"loop","rota":false}]}`)
	if len(d) != 2 || d[0].Kind != "SSD" || d[1].Kind != "Hard disk" {
		t.Fatalf("got %+v", d)
	}
	g := parseLspciGPUs("00:02.0 VGA compatible controller: Intel Corporation UHD Graphics 620\n00:1f.3 Audio device: Intel")
	if len(g) != 1 || g[0] != "Intel Corporation UHD Graphics 620" {
		t.Fatalf("got %v", g)
	}
}

func TestDeviceLabelsPersistAndBaseline(t *testing.T) {
	dir := t.TempDir()
	d := NewDevices(dir, nil, nil)
	d.known["aa:bb:cc:dd:ee:01"] = &deviceRecord{LastIP: "192.168.1.5"}
	d.saveLocked()
	if !d.SetLabel("AA:BB:CC:DD:EE:01", "  Living room TV ") {
		t.Fatal("label not set")
	}
	if d.SetLabel("aa:bb:cc:dd:ee:99", "x") {
		t.Fatal("unknown device accepted")
	}
	d2 := NewDevices(dir, nil, nil)
	if d2.known["aa:bb:cc:dd:ee:01"].Label != "Living room TV" || d2.fresh {
		t.Fatalf("not persisted: %+v fresh=%v", d2.known, d2.fresh)
	}
}
