package services

import (
	"math"
	"testing"
)

func TestValidGeographicPoint(t *testing.T) {
	tests := []struct {
		name string
		lat  float64
		lon  float64
		want bool
	}{
		{name: "valid event fixture", lat: 37.7749, lon: -122.4194, want: true},
		{name: "valid SEI fixture", lat: 37.71, lon: -122.51, want: true},
		{name: "zero zero", lat: 0, lon: 0, want: false},
		{name: "out of range lat 91", lat: 91, lon: -122.4194, want: false},
		{name: "out of range lat -91", lat: -91, lon: 0.1, want: false},
		{name: "out of range lon 181", lat: 10, lon: 181, want: false},
		{name: "out of range lon -181", lat: 10, lon: -181, want: false},
		{name: "NaN lat", lat: math.NaN(), lon: -122.4194, want: false},
		{name: "NaN lon", lat: 37.7749, lon: math.NaN(), want: false},
		{name: "Inf lat", lat: math.Inf(1), lon: -122.4194, want: false},
		{name: "Inf lon", lat: 37.7749, lon: math.Inf(-1), want: false},
		{name: "poles are valid", lat: 90, lon: 180, want: true},
		{name: "equator nonzero lon is valid", lat: 0, lon: 10, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidGeographicPoint(tt.lat, tt.lon)
			if got != tt.want {
				t.Errorf("ValidGeographicPoint(%v, %v) = %v, want %v", tt.lat, tt.lon, got, tt.want)
			}
		})
	}
}

func TestSelectIncidentPosition(t *testing.T) {
	const (
		eventLat = 37.7749
		eventLon = -122.4194
		seiLat   = 37.71
		seiLon   = -122.51
	)
	tests := []struct {
		name     string
		eventLat float64
		eventLon float64
		seiLat   float64
		seiLon   float64
		wantLat  float64
		wantLon  float64
	}{
		{
			name:     "valid event vs different valid SEI prefers event",
			eventLat: eventLat, eventLon: eventLon,
			seiLat: seiLat, seiLon: seiLon,
			wantLat: eventLat, wantLon: eventLon,
		},
		{
			name:     "valid event vs SEI 0,0 prefers event",
			eventLat: eventLat, eventLon: eventLon,
			seiLat: 0, seiLon: 0,
			wantLat: eventLat, wantLon: eventLon,
		},
		{
			name:     "invalid event 0,0 vs valid SEI uses SEI",
			eventLat: 0, eventLon: 0,
			seiLat: seiLat, seiLon: seiLon,
			wantLat: seiLat, wantLon: seiLon,
		},
		{
			name:     "both invalid returns 0,0",
			eventLat: 0, eventLon: 0,
			seiLat: 0, seiLon: 0,
			wantLat: 0, wantLon: 0,
		},
		{
			name:     "out of range event vs valid SEI uses SEI",
			eventLat: 91, eventLon: eventLon,
			seiLat: seiLat, seiLon: seiLon,
			wantLat: seiLat, wantLon: seiLon,
		},
		{
			name:     "NaN event vs valid SEI uses SEI",
			eventLat: math.NaN(), eventLon: eventLon,
			seiLat: seiLat, seiLon: seiLon,
			wantLat: seiLat, wantLon: seiLon,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLat, gotLon := SelectIncidentPosition(tt.eventLat, tt.eventLon, tt.seiLat, tt.seiLon)
			if gotLat != tt.wantLat || gotLon != tt.wantLon {
				t.Errorf("SelectIncidentPosition() = (%v, %v), want (%v, %v)", gotLat, gotLon, tt.wantLat, tt.wantLon)
			}
		})
	}
}
