package services

import (
	"bytes"
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// CoordinateCityPattern matches cities stored as "lat, lon" with 4 decimal places
// (the scanner's fmt.Sprintf("%.4f, %.4f") fallback).
var CoordinateCityPattern = regexp.MustCompile(`^-?\d+\.\d{4}, -?\d+\.\d{4}$`)

//go:embed geodata/au_places_p.tsv.gz
var auPlacesFS embed.FS

// Geocoder resolves lat/lon to a suburb-level place name.
// Implementations must be safe for concurrent use and must not send GPS off-LAN.
type Geocoder interface {
	Reverse(lat, lon float64) (string, error)
}

type place struct {
	name string
	lat  float64
	lon  float64
}

// maxGeocodeDistanceKm: if the nearest AU place is farther than this, keep the
// coordinate string (avoids mapping overseas SEI points onto random AU towns).
const maxGeocodeDistanceKm = 25.0

// OfflineGeocoder is a nearest-neighbour lookup over the embedded GeoNames AU
// populated-places extract. All work stays on-device; no network calls.
type OfflineGeocoder struct {
	places  []place
	once    sync.Once
	loadErr error
	loaded  bool // true when places were supplied (tests) or embed finished
}

var (
	defaultOffline     *OfflineGeocoder
	defaultOfflineOnce sync.Once
)

// NewOfflineGeocoder returns the shared embedded AU places index.
func NewOfflineGeocoder() *OfflineGeocoder {
	defaultOfflineOnce.Do(func() {
		defaultOffline = &OfflineGeocoder{}
		defaultOffline.ensureLoaded()
	})
	return defaultOffline
}

// NewOfflineGeocoderFromGzip builds an index from a gzipped name/lat/lon TSV
// (tests and the generator smoke check).
func NewOfflineGeocoderFromGzip(r io.Reader) (*OfflineGeocoder, error) {
	places, err := loadPlacesGzip(r)
	if err != nil {
		return nil, err
	}
	g := &OfflineGeocoder{places: places, loaded: true}
	// Mark once so ensureLoaded will not overwrite with the embedded AU dump.
	g.once.Do(func() {})
	return g, nil
}

func (g *OfflineGeocoder) ensureLoaded() {
	g.once.Do(func() {
		if g.loaded && len(g.places) > 0 {
			return
		}
		raw, err := auPlacesFS.ReadFile("geodata/au_places_p.tsv.gz")
		if err != nil {
			g.loadErr = err
			return
		}
		places, err := loadPlacesGzip(bytes.NewReader(raw))
		if err != nil {
			g.loadErr = err
			return
		}
		g.places = places
		g.loaded = true
	})
}

func loadPlacesGzip(r io.Reader) ([]place, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	data, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	places := make([]place, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		lat, err1 := strconv.ParseFloat(parts[1], 64)
		lon, err2 := strconv.ParseFloat(parts[2], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		places = append(places, place{name: parts[0], lat: lat, lon: lon})
	}
	if len(places) == 0 {
		return nil, fmt.Errorf("no places loaded")
	}
	return places, nil
}

// CacheKey is kept for tests that round coordinates; offline lookup does not cache.
func CacheKey(lat, lon float64) string {
	return fmt.Sprintf("%.3f,%.3f", math.Round(lat*1000)/1000, math.Round(lon*1000)/1000)
}

// IsCoordinateCity reports whether city looks like the scanner's lat/lon fallback.
func IsCoordinateCity(city string) bool {
	return CoordinateCityPattern.MatchString(city)
}

// Reverse returns the nearest GeoNames AU populated place name.
func (g *OfflineGeocoder) Reverse(lat, lon float64) (string, error) {
	if g == nil {
		return "", fmt.Errorf("geocoder is nil")
	}
	g.ensureLoaded()
	if g.loadErr != nil {
		return "", g.loadErr
	}
	if !ValidGeographicPoint(lat, lon) {
		return "", fmt.Errorf("invalid coordinates")
	}
	if len(g.places) == 0 {
		return "", fmt.Errorf("places index empty")
	}

	best := g.places[0]
	bestDist := equirectangularDist2(lat, lon, best.lat, best.lon)
	for i := 1; i < len(g.places); i++ {
		p := g.places[i]
		d := equirectangularDist2(lat, lon, p.lat, p.lon)
		if d < bestDist {
			bestDist = d
			best = p
		}
	}
	// Convert degree-ish distance to approx km (1 deg lat ≈ 111 km).
	km := math.Sqrt(bestDist) * 111.0
	if km > maxGeocodeDistanceKm {
		return "", fmt.Errorf("nearest place %q is %.1f km away (limit %.0f)", best.name, km, maxGeocodeDistanceKm)
	}
	return best.name, nil
}

// equirectangularDist2 is a fast squared relative distance (degrees).
func equirectangularDist2(lat, lon, plat, plon float64) float64 {
	dLat := plat - lat
	dLon := (plon - lon) * math.Cos(lat*math.Pi/180)
	return dLat*dLat + dLon*dLon
}

// StubGeocoder returns fixed names for tests (no dataset dependency).
type StubGeocoder struct {
	Names map[string]string
	Err   error
}

func (s *StubGeocoder) Reverse(lat, lon float64) (string, error) {
	if s.Err != nil {
		return "", s.Err
	}
	if s.Names == nil {
		return "", fmt.Errorf("no stub names")
	}
	if name, ok := s.Names[CacheKey(lat, lon)]; ok {
		return name, nil
	}
	// Also try exact 4-dp key used in some tests.
	key4 := fmt.Sprintf("%.4f,%.4f", lat, lon)
	if name, ok := s.Names[key4]; ok {
		return name, nil
	}
	return "", fmt.Errorf("stub miss for %s", CacheKey(lat, lon))
}
