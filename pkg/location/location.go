package location

// #cgo CFLAGS: -x objective-c
// #cgo LDFLAGS: -framework Foundation -framework CoreLocation
// #import "location.h"
import "C"
import (
	"fmt"
	"time"
)

// Location encapsulates location services information.
type Location struct {
	// Latitude is the latitude in degrees. Positive values indicate locations
	// north of the equator, while negative values are south of the equator.
	Latitude float64 `json:"latitude"`
	// Longitude is the longitude in degrees. Positive values indicate locations
	// east of the prime meridian, while negative ones are west of the meridian.
	Longitude float64 `json:"longitude"`
	// Altitude is the altitude above mean sea level in meters.
	Altitude float64 `json:"altitude"`
	// EllipsoidalAltitude is the height above the World Geodetic System 1984
	// (WGS84) ellipsoid, in meters.
	EllipsoidalAltitude float64 `json:"ellipsoidal_altitude"`
	// HorizontalAccuracy is the radius of uncertainty for (latitude, longitude)
	// in meters. When less than zero, latitude/longitude location could not
	// be produced.
	HorizontalAccuracy float64 `json:"horizontal_accuracy"`
	// VerticalAccuracy is the estimated uncertainty of altitude values in
	// meters. When less than zero, altitude information could not be produced.
	VerticalAccuracy float64 `json:"vertical_accuracy"`
	// Timestamp is the time when the information was produced.
	Timestamp time.Time `json:"timestamp"`
}

type LocationError int

const (
	LocationErrorCodeUnknown            LocationError = 0
	LocationErrorCodeDenied             LocationError = 1
	LocationErrorCodeNetworkUnavailable LocationError = 2
	LocationErrorCodeHeadingFailure     LocationError = 3
	LocationErrorCodeRangingUnavailable LocationError = 16
	LocationErrorCodeRangingFailure     LocationError = 17
	LocationErrorCodePromptDeclined     LocationError = 18
	LocationErrorCodeSpecialDisabled    LocationError = 64
)

func (e LocationError) Error() string {
	switch e {
	case LocationErrorCodeDenied:
		return "querying location: user denied access to location services"
	case LocationErrorCodeNetworkUnavailable:
		return "querying location: network unavailable or a network error occurred"
	case LocationErrorCodeHeadingFailure:
		return "querying location: location manager cannot determine heading"
	case LocationErrorCodeRangingUnavailable:
		return "querying location: ranging unavailable: device is in airplane mode, device bluetooth is disabled, or location services are disabled"
	case LocationErrorCodeRangingFailure:
		return "querying location: ranging failed: unspecified location services error"
	case LocationErrorCodePromptDeclined:
		return "querying location: user declined temporary authorization"
	case LocationErrorCodeSpecialDisabled:
		return "querying location: location services disabled"
	default:
	}
	return fmt.Sprintf("error querying location: CLLocationManager returned code %d", int(e))
}

func CurrentLocation() (*Location, error) {
	raw := C.Location{}
	if code := C.currentLocation(&raw); code != 0 {
		return nil, LocationError(code)
	}

	loc := Location{
		Latitude:            float64(C.float(raw.latitude)),
		Longitude:           float64(C.float(raw.longitude)),
		Altitude:            float64(C.float(raw.altitude)),
		EllipsoidalAltitude: float64(C.float(raw.ellipsoidalAltitude)),
		HorizontalAccuracy:  float64(C.float(raw.horizontalAccuracy)),
		VerticalAccuracy:    float64(C.float(raw.verticalAccuracy)),
		Timestamp:           time.Unix(int64(C.int(raw.timestamp)), 0),
	}
	return &loc, nil
}
