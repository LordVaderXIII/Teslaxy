package services

import (
	"bytes"
	"compress/gzip"
	"testing"
)

func TestIsCoordinateCity(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"-34.8005, 138.7207", true},
		{"37.1234, -122.5678", true},
		{"Parkside", false},
		{"-34.8005,138.7207", false}, // missing space after comma
		{"-34.800, 138.720", false},  // not 4 decimals
		{"", false},
	}
	for _, c := range cases {
		if got := IsCoordinateCity(c.in); got != c.want {
			t.Errorf("IsCoordinateCity(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestOfflineGeocoder_EmbeddedLookups(t *testing.T) {
	g := NewOfflineGeocoder()
	if g.loadErr != nil {
		t.Fatalf("load: %v", g.loadErr)
	}
	if len(g.places) < 1000 {
		t.Fatalf("expected thousands of AU places, got %d", len(g.places))
	}

	cases := []struct {
		label string
		lat   float64
		lon   float64
		// Accept exact suburb or a known immediate neighbour for edge coords.
		wantAny []string
	}{
		{"recent clip coords", -34.8005, 138.7207, []string{"Fairview Park"}},
		{"Fairview Park area", -34.80, 138.72, []string{"Fairview Park", "Surrey Downs", "Modbury Heights", "Redwood Park"}},
		{"Parkside", -34.945, 138.613, []string{"Parkside", "Fullarton", "Eastwood", "Frewville"}},
	}
	for _, c := range cases {
		name, err := g.Reverse(c.lat, c.lon)
		if err != nil {
			t.Fatalf("%s: %v", c.label, err)
		}
		ok := false
		for _, w := range c.wantAny {
			if name == w {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("%s: got %q, want one of %v", c.label, name, c.wantAny)
		} else {
			t.Logf("%s -> %s", c.label, name)
		}
	}
}

func TestOfflineGeocoder_FromGzip(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte("Testville\t-34.8000\t138.7200\nOther\t-35.0000\t138.0000\n"))
	_ = zw.Close()

	g, err := NewOfflineGeocoderFromGzip(&buf)
	if err != nil {
		t.Fatal(err)
	}
	name, err := g.Reverse(-34.8005, 138.7207)
	if err != nil {
		t.Fatal(err)
	}
	if name != "Testville" {
		t.Fatalf("got %q want Testville", name)
	}
}

func TestOfflineGeocoder_InvalidCoords(t *testing.T) {
	g := NewOfflineGeocoder()
	if _, err := g.Reverse(0, 0); err == nil {
		t.Fatal("expected error for (0,0)")
	}
}
