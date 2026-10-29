package hardware

import "testing"

func TestParseNvidiaSMI(t *testing.T) {
	gpus := ParseNvidiaSMI("NVIDIA GeForce RTX 5070, 12227\nNVIDIA GB10, [N/A]\n")
	if len(gpus) != 2 || gpus[0].Name != "NVIDIA GeForce RTX 5070" || gpus[0].VRAMBytes != 12227<<20 {
		t.Fatalf("%+v", gpus)
	}
	if gpus[1].VRAMBytes != 0 {
		t.Fatal("共用記憶體的顯示卡沒有自己的記憶體")
	}
	if len(ParseNvidiaSMI("")) != 0 {
		t.Fatal("沒有顯示卡")
	}
}

func TestParseMeminfo(t *testing.T) {
	if got := ParseMeminfo("MemTotal:       127600812 kB\nMemFree: 1 kB\n"); got != 127600812<<10 {
		t.Fatal(got)
	}
}

func TestDetectHere(t *testing.T) {
	info := Detect()
	if info.MemoryBytes <= 0 {
		t.Fatalf("這台電腦應該查得到記憶體：%+v", info)
	}
}
