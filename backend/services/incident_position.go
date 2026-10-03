package services

import "math"

// ValidGeographicPoint reports whether lat/lon can be used as a map/incident point.
// (0,0), NaN, Inf, and out-of-range values are rejected so markers are not placed
// at a bogus origin or dropped by clients that treat zero as "missing".
func ValidGeographicPoint(lat, lon float64) bool {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return false
	}
	if lat == 0 && lon == 0 {
		return false
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return false
	}
	return true
}

// SelectIncidentPosition chooses the stored incident marker.
// A valid event.json point always wins over SEI (including a different valid SEI
// midpoint or SEI (0,0)). SEI is only used when the event point is invalid.
// The result is an incident location, not a playback-time vehicle track sample.
func SelectIncidentPosition(eventLat, eventLon, seiLat, seiLon float64) (lat, lon float64) {
	if ValidGeographicPoint(eventLat, eventLon) {
		return eventLat, eventLon
	}
	if ValidGeographicPoint(seiLat, seiLon) {
		return seiLat, seiLon
	}
	return 0, 0
}
