//go:build darwin && !skipnative

package location

// #cgo CFLAGS: -x objective-c
// #cgo LDFLAGS: -framework Foundation -framework CoreLocation
// #import "location.h"
import "C"
import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	p := &placer{}
	cmd := &cobra.Command{
		Use:           "place",
		Short:         "Geolocation information from macOS Location Services",
		RunE:          p.run,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.Flags().BoolVarP(&p.JSON, "json", "j", false, "Print out JSON instead of human-readable text")

	return cmd
}

type placer struct {
	JSON bool
}

func (p *placer) run(cmd *cobra.Command, args []string) error {
	status := AuthorizationStatus()

	loc, err := CurrentLocation()
	if err != nil {
		fmt.Printf("Authorization status: %d\n", status)
		return err
	}

	if p.JSON {
		bs, err := json.MarshalIndent(&loc, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(bs))
		return nil
	}

	fmt.Printf("Latitude: %f\n", loc.Latitude)
	fmt.Printf("Longitude: %f\n", loc.Longitude)
	fmt.Printf("Accuracy: %f\n", loc.HorizontalAccuracy)
	if loc.VerticalAccuracy >= 0 {
		fmt.Printf("Altitude: %f (accuracy: %f)\n", loc.Altitude, loc.VerticalAccuracy)
	}

	fmt.Printf("Last observed: %s\n", loc.Timestamp.Format(time.RFC3339))

	return nil
}

type AuthorizationStatusCode int

const (
	AuthorizationStatusCodeNotDetermined       AuthorizationStatusCode = 0
	AuthorizationStatusCodeRestricted                                  = 1
	AuthorizationStatusCodeDenied                                      = 2
	AuthorizationStatusCodeAuthorizedAlways                            = 3
	AuthorizationStatusCodeAuthorizedWhenInUse                         = 4
)

func AuthorizationStatus() AuthorizationStatusCode {
	code := C.authorizationStatus()
	return AuthorizationStatusCode(code)
}

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

	HasPlacemark bool               `json:"has_placemark"`
	Placemark    *LocationPlacemark `json:"placemark"`
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

type LocationPlacemark struct {
	Name                  string   `json:"name"`
	ISOCountryCode        string   `json:"iso_country_code"`
	Country               string   `json:"country"`
	PostalCode            string   `json:"postal_code"`
	AdministrativeArea    string   `json:"administrative_area"`
	SubadministrativeArea string   `json:"subadministrative_area"`
	Locality              string   `json:"locality"`
	Sublocality           string   `json:"sublocality"`
	Thoroughfare          string   `json:"thoroughfare"`
	Subthoroughfare       string   `json:"subthoroughfare"`
	Region                string   `json:"region"`
	InlandWater           string   `json:"inland_water"`
	Ocean                 string   `json:"ocean"`
	AreasOfInterest       []string `json:"areas_of_interest"`
}

func CurrentLocation() (*Location, error) {
	cloc := C.Location{}
	cpla := C.Placemark{}
	if code := C.currentLocation(&cloc, &cpla); code != 0 {
		return nil, LocationError(code)
	}

	loc := Location{
		Latitude:            float64(C.float(cloc.latitude)),
		Longitude:           float64(C.float(cloc.longitude)),
		Altitude:            float64(C.float(cloc.altitude)),
		EllipsoidalAltitude: float64(C.float(cloc.ellipsoidalAltitude)),
		HorizontalAccuracy:  float64(C.float(cloc.horizontalAccuracy)),
		VerticalAccuracy:    float64(C.float(cloc.verticalAccuracy)),
		Timestamp:           time.Unix(int64(C.int(cloc.timestamp)), 0),

		HasPlacemark: cloc.hasPlacemark == C.BOOL(true),
	}

	if loc.HasPlacemark {
		loc.Placemark = &LocationPlacemark{
			Name:                  NSStringToGoString(cpla.name),
			ISOCountryCode:        NSStringToGoString(cpla.isoCountryCode),
			Country:               NSStringToGoString(cpla.country),
			PostalCode:            NSStringToGoString(cpla.postalCode),
			AdministrativeArea:    NSStringToGoString(cpla.administrativeArea),
			SubadministrativeArea: NSStringToGoString(cpla.subadministrativeArea),
			Locality:              NSStringToGoString(cpla.locality),
			Sublocality:           NSStringToGoString(cpla.sublocality),
			Thoroughfare:          NSStringToGoString(cpla.thoroughfare),
			Subthoroughfare:       NSStringToGoString(cpla.subthoroughfare),
			Region:                NSStringToGoString(cpla.region),
			InlandWater:           NSStringToGoString(cpla.inlandWater),
			Ocean:                 NSStringToGoString(cpla.ocean),
			AreasOfInterest:       NSArrayNSStringToGoStringSlice(cpla.areasOfInterest),
		}
	}

	return &loc, nil
}
