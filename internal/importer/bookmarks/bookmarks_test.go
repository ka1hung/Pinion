package bookmarks

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/traditionalchinese"

	"github.com/ka1hung/pinion/internal/store"
)

const term = "#MobaFont%10%0%0%-1%15%236,236,236%30,30,30%180,180,192%0%-1%0%%xterm%-1%-1%_Std_Colors_0_%80%24%0%1%-1%<none>%%0%1%-1#0# #-1"

const sample = "\ufeff[Misc]\r\nSkin=1\r\n" +
	"[Bookmarks]\r\nSubRep=\r\nImgNum=42\r\n" +
	"core-sw= #109#0%10.0.0.1%22%admin%%-1%-1%%%22%%0%0%0%%%-1%0%0%0%%1080%%0%0%1" + term + "\r\n" +
	"[Bookmarks_1]\r\nSubRep=Site A\\Rack 1\r\nImgNum=41\r\n" +
	"web01= #109#0%192.168.1.10%2222%deploy%%-1%-1%uptime%bastion.example.com__PIPE__10.1.1.1%22__PIPE__2200%jump__PIPE__ops%0%0%0%_ProfileDir_\\.ssh\\id_ed25519%%-1%0%0%0%%1080%%0%0%1" + term + "\r\n" +
	"plc-telnet= #98#1%10.0.0.50%2323%%%2%%%%%0%0%%1080%" + term + "\r\n" +
	"console= #131#8%COM3  (USB-SERIAL CH340 (COM3))%115200%3%2%2%0%0%0%0%0%" + term + "\r\n" +
	"rdp-host= #91#4%10.0.0.9%3389%admin%0%0%0%0%-1%0%0%-1" + term + "\r\n" +
	"local= #131#10%PowerShell%0%" + term + "\r\n" +
	"[Bookmarks_2]\r\nSubRep=Site A\\Empty\r\nImgNum=41\r\n" +
	"[Other]\r\nfoo= #109#0%should.not.import%22%%\r\n"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestParseSample(t *testing.T) {
	r, err := parse([]byte(sample), "windows", env(map[string]string{"USERPROFILE": `C:\Users\me`}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Encoding != "UTF-8 BOM" {
		t.Errorf("encoding %s", r.Encoding)
	}
	byName := map[string]*store.Session{}
	for _, s := range r.Sessions {
		byName[s.Name] = s
	}
	if len(r.Sessions) != 5 {
		t.Fatalf("got %d sessions: %+v", len(r.Sessions), r.Sessions)
	}
	if len(r.Skipped) != 1 || r.Skipped[0].Name != "rdp-host" || r.Skipped[0].Type != "RDP" || r.Skipped[0].Folder != "Site A/Rack 1" {
		t.Errorf("skipped %+v", r.Skipped)
	}
	if strings.Join(r.Folders, ",") != "Site A/Empty,Site A/Rack 1" {
		t.Errorf("folders %v", r.Folders)
	}
	if len(r.Warnings) == 0 {
		t.Error("expected password warning")
	}

	core := byName["core-sw"]
	if core == nil || core.Type != store.TypeSSH || core.Folder != "" || core.SSH.Host != "10.0.0.1" || core.SSH.Port != 22 ||
		core.SSH.User != "admin" || core.SSH.Auth != store.AuthAuto || len(core.SSH.JumpHosts) != 0 || core.SSH.KeyPath != "" {
		t.Errorf("core-sw %+v %+v", core, core.SSH)
	}

	web := byName["web01"]
	if web.Folder != "Site A/Rack 1" || web.SSH.Port != 2222 || web.SSH.User != "deploy" || web.SSH.StartupCommand != "uptime" {
		t.Errorf("web01 %+v %+v", web, web.SSH)
	}
	if web.SSH.Auth != store.AuthKey || web.SSH.KeyPath != `C:\Users\me\.ssh\id_ed25519` {
		t.Errorf("key %q auth %q", web.SSH.KeyPath, web.SSH.Auth)
	}
	want := []store.JumpHost{{Host: "bastion.example.com", Port: 22, User: "jump"}, {Host: "10.1.1.1", Port: 2200, User: "ops"}}
	if len(web.SSH.JumpHosts) != 2 || web.SSH.JumpHosts[0] != want[0] || web.SSH.JumpHosts[1] != want[1] {
		t.Errorf("jumps %+v", web.SSH.JumpHosts)
	}

	tel := byName["plc-telnet"]
	if tel.Type != store.TypeTelnet || tel.Telnet.Host != "10.0.0.50" || tel.Telnet.Port != 2323 {
		t.Errorf("telnet %+v", tel.Telnet)
	}

	ser := byName["console"]
	if ser.Type != store.TypeSerial || ser.Serial.Port != "COM3" || ser.Serial.Baud != 115200 || ser.Serial.DataBits != 8 ||
		ser.Serial.Parity != "even" || ser.Serial.StopBits != "2" {
		t.Errorf("serial %+v", ser.Serial)
	}

	loc := byName["local"]
	if loc.Type != store.TypeLocal || loc.Local.Shell != "powershell.exe" {
		t.Errorf("local %+v", loc.Local)
	}
}

func TestNonWindowsKeyPathWarning(t *testing.T) {
	r, err := parse([]byte(sample), "darwin", env(nil))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range r.Sessions {
		if s.Name == "web01" && s.SSH.KeyPath == `_ProfileDir_\.ssh\id_ed25519` {
			found = true
		}
		if s.Type == store.TypeLocal && s.Local.Shell != "" {
			t.Error("windows shell kept on darwin")
		}
	}
	if !found || len(r.Warnings) != 2 {
		t.Errorf("found=%v warnings=%v", found, r.Warnings)
	}
}

func TestBig5(t *testing.T) {
	src := "[Bookmarks]\r\nSubRep=台北案場\\機房\r\nImgNum=41\r\n" +
		"交換機= #109#0%10.1.2.3%22%管理員%%-1%-1%%%22%%0%0%0%%%-1%0%0%0%%1080%%0%0%1" + term + "\r\n"
	b, err := traditionalchinese.Big5.NewEncoder().Bytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Encoding != "Big5" {
		t.Fatalf("encoding %s", r.Encoding)
	}
	if len(r.Sessions) != 1 {
		t.Fatalf("%+v", r)
	}
	s := r.Sessions[0]
	if s.Name != "交換機" || s.Folder != "台北案場/機房" || s.SSH.User != "管理員" {
		t.Errorf("%+v %+v", s, s.SSH)
	}
	if len(r.Folders) != 1 || r.Folders[0] != "台北案場/機房" {
		t.Errorf("folders %v", r.Folders)
	}
}

func TestEscapes(t *testing.T) {
	src := "[Bookmarks]\nSubRep=\nmy__PERCENT__box= #109#0%h%22%u%%-1%-1%echo a__PTVIRG__ echo __DBLQUO__b__DBLQUO__ __DIEZE__c%" + term + "\n"
	r, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	s := r.Sessions[0]
	if s.Name != "my%box" || s.SSH.StartupCommand != `echo a; echo "b" #c` {
		t.Errorf("%q %q", s.Name, s.SSH.StartupCommand)
	}
}

func TestHelpers(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"COM12  (Prolific USB-to-Serial Comm Port (COM12))", "COM12"},
		{"com4", "COM4"},
		{"/dev/ttyUSB0 (FT232R)", "/dev/ttyUSB0"},
	}
	for _, c := range cases {
		if got := serialPort(c.in); got != c.want {
			t.Errorf("serialPort(%q)=%q", c.in, got)
		}
	}
	for in, want := range map[string]int{"0": 5, "1": 6, "2": 7, "3": 8, "7": 7, "8": 8, "x": 8} {
		if got := dataBits(in); got != want {
			t.Errorf("dataBits(%s)=%d", in, got)
		}
	}
	for in, want := range map[string]string{"0": "none", "1": "odd", "2": "even", "3": "mark", "4": "space"} {
		if got := parity(in); got != want {
			t.Errorf("parity(%s)=%s", in, got)
		}
	}
	for in, want := range map[string]string{"0": "1", "1": "1.5", "2": "2"} {
		if got := stopBits(in); got != want {
			t.Errorf("stopBits(%s)=%s", in, got)
		}
	}
	if got := expandPath(`_CurrentDrive_:\keys\a.ppk`, "windows", env(map[string]string{"SystemDrive": "D:"})); got != `D:\keys\a.ppk` {
		t.Errorf("expand %q", got)
	}
}

func TestErrors(t *testing.T) {
	if _, err := Parse([]byte("[Misc]\nfoo=bar\n")); err == nil {
		t.Error("expected error without Bookmarks")
	}
	r, err := Parse([]byte("[Bookmarks]\nbroken=nothing here\nnohost= #109#0%%22%\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sessions) != 0 || len(r.Skipped) != 2 {
		t.Errorf("%+v", r)
	}
}
