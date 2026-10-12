package main

import "testing"

func TestInstaladorEmPastaDeDownload(t *testing.T) {
	home := "/Users/ana"
	casos := map[string]bool{
		"/Users/ana/Downloads/Instalador Lumnia Zap.app":                 true,
		"/Users/ana/Downloads/zap/Instalador Lumnia Zap.app":             true,
		"/Users/ana/Desktop/Instalador Lumnia Zap.app":                   true,
		"/Applications/Instalador Lumnia Zap.app":                        false,
		"/Users/ana/Developer/lumnia-zap/dist/Instalador Lumnia Zap.app": false,
		"/Users/ana/DownloadsX/Instalador Lumnia Zap.app":                false,
	}
	for app, quer := range casos {
		if got := instaladorEmPastaDeDownload(home, app); got != quer {
			t.Errorf("%s: %v, queria %v", app, got, quer)
		}
	}
}

func TestJsString(t *testing.T) {
	if got := jsString(`/Users/ana/Downloads/a "b"\c.app`); got != `"/Users/ana/Downloads/a \"b\"\\c.app"` {
		t.Errorf("jsString: %s", got)
	}
}
